package durable

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Official f9a26365 harness-continue.test.ts:121-150. A real registered tool
// waits for cancellation; the real conversation abort supplies its terminal
// result. The next provider request, not a fabricated context, must answer it.
func TestMainContinueAfterAbortedToolRound(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		started := make(chan struct{})
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{
			Definition:     goai.Tool{Name: "wait", Parameters: json.RawMessage(`{"type":"object"}`)},
			Implementation: "continue.abort.wait", Version: 1,
			Execute: func(ctx context.Context, _ JSON, _ *ToolAPI) (ToolResult, error) {
				close(started)
				<-ctx.Done()
				return ToolResult{}, ctx.Err()
			},
		}); err != nil {
			t.Fatal(err)
		}
		seen := make(chan []goai.Message, 1)
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, request *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				ch <- toolAnswer("c1", "wait", JSON{})
			} else {
				seen <- request.Messages
				ch <- terminal("after the abort")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, started)
		if err := c.Abort(bg); err != nil {
			t.Fatal(err)
		}
		if got := waitSubmission(t, first); got.Record.Status != "unanswered" || got.Record.Reason != "aborted" {
			t.Fatal(got)
		}
		continued, err := c.Submit(bg, Input{Type: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		got := waitSubmission(t, continued)
		if got.Record.Status != "done" || got.Message == nil || len(got.Message.Content) != 1 || got.Message.Content[0].Text != "after the abort" {
			t.Fatal(got)
		}
		messages := watchReceive(t, seen)
		if len(messages) == 0 || messages[len(messages)-1].Role != goai.RoleToolResult || calls.Load() != 2 {
			t.Fatal("continue did not answer the aborted round's tool result", messages, calls.Load())
		}
	})
}
