package durable

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// codemode.test.ts:1075–1092. Deferred controls model exposure, not nested
// execution or guest catalogue membership. Check both boundaries of the same
// public SQLite turn with the published portable guest.
func TestSourceV160CodemodePublishedWorkerDeferredTool(t *testing.T) {
	const source = `return [await tools.later({}), ALL_TOOLS.some((tool) => tool.name === "later")];`
	sandbox, remote := newJournalWorkerSandboxV160(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	registry := NewRegistry()
	codemode, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	later := wrapRegistration("later")
	later.Deferred = true
	later.Definition.Description = "A deferred tool"
	later.Definition.Parameters = json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
	later.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
		calls.Add(1)
		return ToolResult{Content: "later ran"}, nil
	}
	if err := registry.Register(later); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	offers := make(chan []string, 2)
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		req := requests.Add(1)
		if req <= 2 {
			names := make([]string, 0, len(input.Tools))
			for _, tool := range input.Tools {
				names = append(names, tool.Name)
			}
			offers <- names
		}
		ch := make(chan goai.Event, 1)
		if req == 1 {
			ch <- toolAnswer("deferred", "codemode", JSON{"code": source})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	sub, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if settled, err := sub.Wait(ctx); err != nil || settled.Submission.Status != "done" {
		t.Fatalf("submission status=%q err=%v; want done", settled.Submission.Status, err)
	}
	result := v160ToolResult(t, ctx, conversation, "deferred")
	const want = `["later ran",true]`
	if text := v160ToolResultText(result); result.IsError || !strings.Contains(text, want) { // L1091
		t.Errorf("script result=%q, error=%t; want %s", text, result.IsError, want)
	}
	if requests.Load() != 2 || calls.Load() != 1 || remote.starts.Load() != 1 {
		t.Errorf("requests=%d, deferred effects=%d, guest starts=%d; want 2, 1, 1", requests.Load(), calls.Load(), remote.starts.Load())
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	close(offers)
	for names := range offers {
		if slices.Contains(names, "later") || !slices.Contains(names, "codemode") {
			t.Errorf("provider tool offers = %v; want codemode and no direct later offer", names)
		}
	}
}
