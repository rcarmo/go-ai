package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResetIdleAndPostToolsStartsFollowUpInNewContext(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered, release := make(chan struct{}), make(chan struct{})
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "hold", Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "reset.hold", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			close(entered)
			<-release
			return ToolResult{Content: "old tool"}, nil
		}}); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				ch <- toolAnswer("old-call", "hold", JSON{})
			} else {
				text := []string{}
				for _, message := range input.Messages {
					for _, block := range message.Content {
						text = append(text, block.Text)
					}
				}
				if strings.Join(text, ",") != "handoff,follow" {
					t.Error("reset retained old context", text)
				}
				ch <- terminal("fresh")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		options.OnReport = func(err error) { t.Error("scheduler", err) }
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		conversation := root(t, h, ref)
		if err := conversation.Reset(bg, "idle"); err != nil {
			t.Fatal(err)
		}
		view, err := conversation.ContextView(bg, 0)
		if err != nil || len(view.Messages) != 1 {
			t.Fatal(view, err)
		}
		first, err := conversation.Submit(bg, Input{Content: "old"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		follow, err := conversation.Submit(bg, Input{Content: "follow"})
		if err != nil {
			t.Fatal(err)
		}
		if err := conversation.Reset(bg, "handoff"); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		old := waitSubmission(t, first)
		fresh := waitSubmission(t, follow)
		if old.Record.Status != "unanswered" || old.Record.Reason != "reset" || old.Record.Answer != 0 || fresh.Record.Status != "done" || calls.Load() != 2 {
			t.Fatal(old, fresh, calls.Load())
		}
		history, err := conversation.Entries(bg, EntryQuery{Order: Ascending}, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		var kinds []string
		for _, entry := range history.Items {
			if entry.Kind != "pi.system" {
				kinds = append(kinds, entry.Kind)
			}
		}
		if !reflect.DeepEqual(kinds, []string{"pi.reset", "pi.user", "pi.assistant", "pi.tool-result", "pi.reset", "pi.user", "pi.assistant"}) {
			t.Fatal("reset transcript ordering", kinds)
		}
		oldGeneration, oldAssistant, oldReceipts := ID(0), ID(0), 0
		for _, entry := range history.Items {
			if entry.Kind == "pi.assistant" && len(entry.Model) == 1 && entry.Model[0].StopReason == goai.StopReasonToolUse {
				oldGeneration, oldAssistant = entry.ByTask, entry.ID
			}
		}
		if oldGeneration == 0 || oldAssistant == 0 {
			t.Fatal("actual tool-call assistant attribution missing", history.Items)
		}
		record, found, err := h.Task(bg, oldGeneration)
		if err != nil || !found || record.Kind != "pi.generation" || record.Version != 2 || record.State.Outcome == nil || record.State.Outcome.Status != "completed" || record.State.Outcome.Result == nil || submissionEntryID(generationObject(record.State.Outcome.Result.Value)["entryId"]) != oldAssistant {
			t.Fatal("post-tools reset changed completed generation", record, err)
		}
		resetCount := 0
		for _, entry := range history.Items {
			if entry.ByTask == oldGeneration && entry.Kind == "pi.assistant" {
				oldReceipts++
			}
			if entry.Kind == "pi.reset" {
				resetCount++
				if entry.Head != entry.ID {
					t.Fatal("reset head", entry)
				}
			}
		}
		if resetCount != 2 || oldReceipts != 1 {
			t.Fatal("reset placement or synthetic assistant", resetCount, oldReceipts)
		}
		public, err := first.Status(bg)
		if err != nil || public.Status != "unanswered" || public.Reason != "reset" || public.Answer != 0 {
			t.Fatal("reset settlement projection", public, err)
		}
	})
}
func TestResetAtFinalBoundaryKeepsAnswerThenCutsContext(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		entered, release := make(chan struct{}), make(chan struct{})
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			go func() { close(entered); <-release; ch <- terminal("answer"); close(ch) }()
			return ch
		})
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		conversation := root(t, h, ref)
		sub, err := conversation.Submit(bg, Input{Content: "old"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		if err := conversation.Reset(bg, "new"); err != nil {
			t.Fatal(err)
		}
		releaseTaskGate(release)
		settled := waitSubmission(t, sub)
		if settled.Record.Status != "done" || settled.Message == nil || len(settled.Message.Content) != 1 || settled.Message.Content[0].Text != "answer" {
			t.Fatal(settled)
		}
		view, err := conversation.ContextView(bg, 0)
		if err != nil || len(view.Messages) != 1 || view.Messages[0].Role != goai.RoleUser || view.Messages[0].ContentText == nil || *view.Messages[0].ContentText != "new" || len(view.Messages[0].Content) != 0 {
			t.Fatal("final reset string-content context", view, err)
		}
		entries, err := conversation.Entries(bg, EntryQuery{Order: Ascending}, 100, nil)
		if err != nil || len(entries.Items) != 3 || entries.Items[0].Kind != "pi.user" || entries.Items[1].Kind != "pi.assistant" || entries.Items[2].Kind != "pi.reset" || entries.Items[2].Head != entries.Items[2].ID {
			t.Fatal("final reset transcript ordering", entries, err)
		}
	})
}

// Pinned harness-inbox.test.ts291-306: idle reset has no model when omitted;
// handoff content stays a string and uses the host timestamp exactly.
func TestIdleResetCanonicalStringContentAndClock(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := openHarness(t, b.store, Options{Now: func() int64 { return 7 }})
		c, err := h.Root(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Commit(bg, func(tx *Tx) (any, error) { return tx.AppendEntry(c.ID(), JSON{"kind": "note"}) }); err != nil {
			t.Fatal(err)
		}
		if err := c.Reset(bg, ""); err != nil {
			t.Fatal(err)
		}
		view, err := c.ContextView(bg, 0)
		if err != nil || view.Head == nil || view.Head.Kind != "pi.reset" || len(view.Head.Model) != 0 || len(view.Messages) != 0 {
			t.Fatal("empty reset", view, err)
		}
		if err := c.Reset(bg, "carry on"); err != nil {
			t.Fatal(err)
		}
		view, err = c.ContextView(bg, 0)
		if err != nil || view.Head == nil || view.Head.Head != view.Head.ID || len(view.Messages) != 1 {
			t.Fatal("handoff reset", view, err)
		}
		wire, err := dtoObject(view.Messages[0], DefaultLimits())
		if err != nil || !equalJSONValue(wire, JSON{"role": "user", "content": "carry on", "timestamp": 7}) {
			t.Fatal("reset model message", wire, err)
		}
	})
}
