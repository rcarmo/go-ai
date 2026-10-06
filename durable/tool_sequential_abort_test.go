package durable

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestSequentialRoundAbortHasNoLaterTaskAndOneUnstartedResult(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		entered, release := make(chan struct{}), make(chan struct{})
		var effects atomic.Int64
		for _, name := range []string{"first", "second"} {
			name := name
			reg := wrapRegistration(name)
			reg.Execute = func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
				effects.Add(1)
				if name == "first" {
					close(entered)
					<-release
					return ToolResult{}, ctx.Err()
				}
				t.Error("unstarted sequential effect ran")
				return ToolResult{}, nil
			}
			if err := registry.Register(reg); err != nil {
				t.Fatal(err)
			}
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			answer := toolAnswer("first-call", "first", JSON{})
			answer.Message.Content = append(answer.Message.Content, goai.ContentBlock{Type: "toolCall", ID: "second-call", Name: "second", Arguments: map[string]any{}})
			ch <- answer
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		conversation, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{ToolExecution: "sequential"}})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conversation.Submit(bg, Input{Content: "abort round"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		aborting := make(chan error, 1)
		go func() { aborting <- conversation.Abort(bg) }()
		deadline, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		for {
			state, err := h.Snapshot(deadline)
			if err != nil {
				t.Fatal(err)
			}
			marked := false
			for _, task := range state.Tasks {
				if task.Kind == "pi.tool" && taskAborted(task) {
					marked = true
				}
			}
			if marked {
				break
			}
		}
		close(release)
		select {
		case err := <-aborting:
			if err != nil {
				t.Fatal(err)
			}
		case <-deadline.Done():
			t.Fatal("sequential abort did not join", deadline.Err())
		}
		result := waitSubmission(t, sub)
		if result.Submission.Status != "aborted" || effects.Load() != 1 {
			t.Fatal(result, effects.Load())
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		tools, laterResults := 0, 0
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				tools++
			}
		}
		for _, entry := range state.Entries {
			if entry.Kind != "message" {
				continue
			}
			var receipt MessageReceipt
			if err := fromObject(entry.Value, &receipt, h.session.limits); err == nil && receipt.Role == goai.RoleToolResult && receipt.ToolCallID == "second-call" {
				if receipt.ErrorCode != "aborted" || !receipt.IsError {
					t.Fatal("unstarted abort result", receipt)
				}
				laterResults++
			}
		}
		if tools != 1 || laterResults != 1 {
			t.Fatal("abort precreated/replayed unstarted call", tools, laterResults)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		resumed, err := h.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		if result := waitSubmission(t, resumed); result.Submission.Status != "aborted" || effects.Load() != 1 {
			t.Fatal("aborted round replay", result, effects.Load())
		}
	})
}
