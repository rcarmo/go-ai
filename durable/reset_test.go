package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestResetIdleAndPostToolsStartsFollowUpInNewContext(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
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
		if old.Submission.Status != "aborted" || old.Submission.Value["errorCode"] != "reset" || fresh.Submission.Status != "done" || calls.Load() != 2 {
			t.Fatal(old, fresh, calls.Load())
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		resetCount := 0
		for _, entry := range state.Entries {
			if entry.Kind == "pi.reset" {
				resetCount++
				if entry.Head != entry.ID {
					t.Fatal("reset head", entry)
				}
			}
		}
		if resetCount != 2 {
			t.Fatal("reset placement", resetCount)
		}
	})
}
func TestResetAtFinalBoundaryKeepsAnswerThenCutsContext(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
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
		if settled.Submission.Status != "done" || settled.Message.Content[0].Text != "answer" {
			t.Fatal(settled)
		}
		view, err := conversation.ContextView(bg, 0)
		if err != nil || len(view.Messages) != 1 || view.Messages[0].Content[0].Text != "new" {
			t.Fatal("final reset context", view, err)
		}
	})
}
