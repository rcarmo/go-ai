package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
	"time"
)

// Pause after VM completion but before codemode settlement. The nested delivery
// is confirmed; reopening must replay it without another external effect.
func TestCodemode120ModelToolUnsettledReopen(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		sandbox := &restartQuickJS120{sandbox: engine, ready: make(chan struct{})}
		registry := NewRegistry()
		var effects, requests atomic.Int64
		leaf := wrapRegistration("recovery_leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "recovered-leaf"}, nil
		}
		if err = registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		extension, err := NewCodemodeExtension(sandbox)
		if err != nil {
			t.Fatal(err)
		}
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "recovery-call", Name: "codemode", Arguments: map[string]any{"code": `text(await tools.recovery_leaf({}));`}}}}
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
			} else {
				last := input.Messages[len(input.Messages)-1]
				if last.Role != goai.RoleToolResult || goai.ContentText(last.Content) != "recovered-leaf" {
					t.Error("recovered transcript", last)
				}
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		sub, err := conv.Submit(bg, Input{Content: "recover script"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, sandbox.ready)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, doc := range state.Documents {
			if doc.Kind == codemodeJournalKind {
				var journal codemodeJournalState
				if err = fromObject(doc.Value, &journal, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if len(journal.Issued) != 1 || len(journal.Deliveries) != 1 {
					t.Fatal("nested delivery not confirmed", journal)
				}
				found = true
			}
		}
		if !found || effects.Load() != 1 || requests.Load() != 1 {
			t.Fatal("wrong interruption boundary", found, effects.Load(), requests.Load())
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		recovered, err := next.Conversation(bg, conv.ID())
		if err != nil {
			t.Fatal(err)
		}
		// Resume pending work through the same persisted submission handle identity.
		if err = next.Resume(bg); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 10*time.Second)
		defer cancel()
		reopenedSub, err := next.Submission(ctx, sub.ID())
		if err != nil || reopenedSub == nil {
			t.Fatal("submission missing", err)
		}
		result, err := reopenedSub.Wait(ctx)
		if err != nil || result.Submission.Status != "done" || effects.Load() != 1 || requests.Load() != 2 || sandbox.starts.Load() != 2 {
			t.Fatal(result, err, effects.Load(), requests.Load(), sandbox.starts.Load())
		}
		view, err := recovered.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		calls, results := 0, 0
		for _, message := range view.Messages {
			for _, block := range message.Content {
				if block.Type == "toolCall" && block.Name == "codemode" {
					calls++
				}
			}
			if message.Role == goai.RoleToolResult && message.ToolName == "codemode" {
				results++
			}
		}
		if calls != 1 || results != 1 {
			t.Fatal("duplicate recovered transcript", calls, results)
		}
	})
}
