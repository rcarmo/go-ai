package durable

import (
	"context"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type countedQuickJSV160 struct {
	*QuickJSSandbox
	starts atomic.Int32
}

func (s *countedQuickJSV160) Start(ctx context.Context, source string, start CodemodeStart) (CodemodeExecution, error) {
	s.starts.Add(1)
	return s.QuickJSSandbox.Start(ctx, source, start)
}

// codemode.test.ts:858–886. Cut off the public model tool after execution has
// committed its Codemode outcome but before AfterTool has let it publish.
func TestSourceV160CodemodeDecidedOutcomeReadback(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 12*time.Second)
	defer cancel()
	engine, err := NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	sandbox := &countedQuickJSV160{QuickJSSandbox: engine}
	codemode, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	var block atomic.Bool
	block.Store(true)
	reached := make(chan struct{})
	codemode.ToolHooks = ToolHooks{AfterTool: func(ctx context.Context, call goai.ToolCall, _ ToolResult, _ *HookAPI) (*ToolResult, error) {
		if call.Name == "codemode" && block.Load() {
			close(reached)
			<-ctx.Done()
		}
		return nil, nil
	}}
	registry := NewRegistry()
	if err := registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("s1", "codemode", JSON{"code": `return "first"`})
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
	if err = h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reached:
	case <-ctx.Done():
		t.Fatalf("AfterTool cutoff not reached: %v", ctx.Err())
	}
	snap, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	runs := 0
	for _, doc := range snap.Documents {
		if doc.Kind != codemodeRunKind {
			continue
		}
		runs++
		var run codemodeRunState
		if err := decodeDocumentValue(doc, DefaultLimits(), &run); err != nil {
			t.Fatal(err)
		}
		if run.Result == nil || !run.Result.HasValue || run.Result.Value != "first" || run.Result.Output != "first" || run.Result.Error != "" {
			t.Fatalf("decided outcome at AfterTool = %+v", run.Result)
		}
	}
	if runs != 1 {
		t.Fatalf("Codemode run documents at cutoff = %d; want 1", runs)
	}
	// A decided outcome is not yet a published result. Check the public
	// transcript at the barrier rather than inferring it from a later wait.
	entries, err := conversation.Entries(ctx, EntryQuery{}, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries.Items {
		for _, message := range entry.Model {
			if message.Role == goai.RoleToolResult && message.ToolCallID == "s1" {
				t.Fatal("Codemode result was published before AfterTool returned")
			}
		}
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	block.Store(false)
	next := openHarness(t, reopenStoreAfterHarnessClose(t, store), options)
	if err := next.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := next.Submission(ctx, sub.ID())
	if err != nil || reopened == nil {
		t.Fatalf("reopen submission: %v, %v", reopened, err)
	}
	if result, err := reopened.Wait(ctx); err != nil || result.Submission.Status != "done" {
		t.Fatalf("reopened submission: %+v, %v", result, err)
	}
	conversation, err = next.Conversation(ctx, conversation.ID())
	if err != nil {
		t.Fatal(err)
	}
	if starts := sandbox.starts.Load(); starts != 1 {
		t.Fatalf("QuickJS starts = %d; want 1", starts)
	}
	result := v160ToolResult(t, ctx, conversation, "s1")
	header, output, found := strings.Cut(v160ToolResultText(result), "\nOutput:\n")
	if result.IsError || !found || !regexp.MustCompile(`^Script completed\nWall time [\d.]+ seconds$`).MatchString(header) || output != "first" {
		t.Fatalf("published result = %q, isError = %t; want first", v160ToolResultText(result), result.IsError)
	}
}
