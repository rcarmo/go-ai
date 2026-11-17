package durable

import (
	"context"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSequentialRoundAbortHasNoLaterTasksAndOrderedUnstartedResults(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		registry := NewRegistry()
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var effects atomic.Int64
		for _, name := range []string{"first", "second"} {
			name := name
			reg := wrapRegistration(name)
			reg.Execute = func(ctx context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
				effects.Add(1)
				if name == "first" {
					close(entered)
					<-ctx.Done()
					close(cancelled)
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
			answer.Message.Content = append(answer.Message.Content, goai.ContentBlock{Type: "toolCall", ID: "second-call", Name: "second", Arguments: map[string]any{}}, goai.ContentBlock{Type: "toolCall", ID: "third-call", Name: "second", Arguments: map[string]any{}})
			ch <- answer
			close(ch)
			return ch
		})
		options.Registry = registry
		options.Settings = &HarnessSettings{ToolExecution: "sequential"}
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		conversation, err := h.Root(bg, AgentChange{Model: ref})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conversation.Submit(bg, Input{Content: "abort round"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var generation ID
		for _, doc := range before.Documents {
			if doc.Kind != "pi.live" || doc.Owner != conversation.ID() {
				continue
			}
			generation = submissionEntryID(generationObject(doc.Value["run"])["taskId"])
			slots, _ := doc.Value["tools"].([]any)
			if len(slots) != 3 || submissionEntryID(generationObject(slots[0])["taskId"]) == 0 || generationObject(slots[1])["taskId"] != nil || generationObject(slots[2])["taskId"] != nil {
				t.Fatal("sequential started/pending slots", slots)
			}
		}
		if generation == 0 {
			t.Fatal("generation missing from canonical run")
		}
		if disposition, err := h.AbortTask(bg, generation); err != nil || disposition != "marked" {
			t.Fatal("abort generation", disposition, err)
		}
		awaitTaskSignal(t, cancelled)
		releaseTaskGate(release)
		result := waitSubmission(t, sub)
		if result.Record.Status != "unanswered" || result.Record.Reason != "aborted" || effects.Load() != 1 {
			t.Fatal(result, effects.Load())
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		tools, laterResults := 0, 0
		var order []string
		for _, task := range state.Tasks {
			if task.Kind == "pi.tool" {
				tools++
			}
		}
		for _, id := range ids(state.Entries) {
			entry, found, err := h.Entry(bg, id)
			if err != nil || !found {
				t.Fatal("public entry lookup", id, err)
			}
			if entry.Kind != "pi.tool-result" {
				continue
			}
			if len(entry.Model) != 1 || entry.Model[0].Role != goai.RoleToolResult || !entry.Model[0].IsError {
				t.Fatal("canonical aborted tool result", entry)
			}
			receipt := entry.Model[0]
			order = append(order, receipt.ToolCallID)
			if receipt.ToolCallID == "second-call" || receipt.ToolCallID == "third-call" {
				want := []goai.ContentBlock{{Type: "text", Text: "<harness>\n[error] Tool second was aborted\n</harness>"}}
				if receipt.ErrorCode != "" || !equalJSONValue(receipt.Content, want) || !entry.HasData || !equalJSONValue(entry.Data, JSON{"diagnostics": []any{JSON{"severity": "error", "code": "aborted", "message": "Tool second was aborted"}}}) {
					t.Fatal("unstarted abort result", entry)
				}
				laterResults++
			}
		}
		if tools != 1 || laterResults != 2 || !equalJSONValue(order, []string{"first-call", "second-call", "third-call"}) {
			t.Fatal("abort precreated/replayed unstarted call", tools, laterResults, order)
		}
		record, found, err := h.Task(bg, generation)
		if err != nil || !found || record.State.Outcome == nil || record.State.Outcome.Status != "aborted" {
			t.Fatal("generation did not settle aborted", record, err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		resumed, err := h.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		if result := waitSubmission(t, resumed); result.Record.Status != "unanswered" || result.Record.Reason != "aborted" || effects.Load() != 1 {
			t.Fatal("aborted round replay", result, effects.Load())
		}
	})
}
