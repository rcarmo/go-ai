package durable

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

const v160UndefinedJournalScript = `const before = await nothing(); await tools.hold({}); return [before === undefined, (await nothing()) === undefined];`

// codemode.test.ts:247–258 and 663–671. Compare the published text bytes,
// stripping only the status/time envelope used by the original resultText.
func TestSourceV160CodemodeGlobalJournalReadback(t *testing.T) {
	t.Run("counter", func(t *testing.T) {
		testV160CodemodeJournalReadback(t, `const n = await next(); await tools.hold({}); return [n, await next()];`, "next", "[1,2]", CodemodeUndefined, false)
	})
	t.Run("undefined", func(t *testing.T) {
		testV160CodemodeJournalReadback(t, v160UndefinedJournalScript, "nothing", "[true,true]", CodemodeUndefined, false)
	})
}

// The native serial runner uses a separate guest callback path.
func TestV160CodemodeSerialUndefinedGlobalJournalReadback(t *testing.T) {
	testV160CodemodeJournalReadback(t, v160UndefinedJournalScript, "nothing", "[true,true]", CodemodeUndefined, true)
}

// Supplemental: valid old explicit-null argument projections and their
// numeric readback remain distinct from the new omitted argument encoding.
func TestV160CodemodeExplicitNullGlobalJournalReadback(t *testing.T) {
	testV160CodemodeJournalReadback(t, `const n = await next(null); await tools.hold({}); return [n, await next(null)];`, "next", "[1,2]", nil, false)
}

func testV160CodemodeJournalReadback(t *testing.T, source, global, want string, wantArgument any, serial bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 12*time.Second)
	defer cancel()
	engine, err := NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	engine.ParallelCalls = !serial
	testV160CodemodeJournalWithSandbox(t, ctx, engine, source, global, want, wantArgument)
}

func testV160CodemodeJournalWithSandbox(t *testing.T, ctx context.Context, sandbox CodemodeSandbox, source, global, want string, wantArgument any) {
	t.Helper()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	codemode, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	var block atomic.Bool
	block.Store(true)
	var calls, holds, requests atomic.Int32
	entered := make(chan struct{})
	hold := wrapRegistration("hold")
	hold.Execute = func(ctx context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
		holds.Add(1)
		if block.Load() {
			close(entered)
			<-ctx.Done()
			return ToolResult{}, ctx.Err()
		}
		return ToolResult{Content: "held"}, nil
	}
	if err = registry.Register(hold); err != nil {
		t.Fatal(err)
	}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("s1", "codemode", JSON{"code": source})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	options.CodemodeGlobals = []CodemodeGlobal{{Name: global, Version: 1, Execute: func(_ context.Context, argument any) (any, error) {
		if argument != wantArgument {
			return nil, fmt.Errorf("argument = %#v; want %#v", argument, wantArgument)
		}
		n := calls.Add(1)
		if global == "nothing" {
			return CodemodeUndefined, nil
		}
		return n, nil
	}}}
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	sub, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	settled := make(chan error, 1)
	go func() { _, err := sub.Wait(ctx); settled <- err }()
	select {
	case <-entered:
	case err := <-settled:
		if err != nil {
			t.Fatalf("submission stopped before hold: %v", err)
		}
		t.Fatalf("submission stopped before hold: %q", v160ToolResultText(v160ToolResult(t, ctx, conversation, "s1")))
	case <-ctx.Done():
		t.Fatalf("hold not reached: %v", ctx.Err())
	}
	if calls.Load() != 1 || holds.Load() != 1 {
		t.Fatalf("before close: globals=%d holds=%d", calls.Load(), holds.Load())
	}
	// Supplemental readback: the exact admitted argument shape and undefined
	// result are already durable before a later call enters its executor.
	state, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	journals := 0
	for _, doc := range state.Documents {
		if doc.Kind != codemodeJournalKind {
			continue
		}
		journals++
		var journal codemodeJournalState
		if err := decodeDocumentValue(doc, DefaultLimits(), &journal); err != nil {
			t.Fatal(err)
		}
		if len(journal.Deliveries) != 1 {
			t.Fatalf("deliveries before close = %+v", journal.Deliveries)
		}
		delivery := journal.Deliveries[0]
		argument, hasArgument := delivery.Call.Arguments["args"]
		omitted := wantArgument == CodemodeUndefined
		if delivery.Seq != 1 || delivery.Call.Name != global || !delivery.Call.Global ||
			delivery.Call.UndefinedArgument != omitted || hasArgument == omitted || argument != nil ||
			delivery.HasValue != (global != "nothing") {
			t.Fatalf("unexpected committed delivery before hold: %+v", delivery)
		}
	}
	if journals != 1 {
		t.Fatalf("journals before close = %d; want 1", journals)
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
		t.Fatalf("replayed submission: %+v, %v", result, err)
	}
	conversation, err = next.Conversation(ctx, conversation.ID())
	if err != nil {
		t.Fatal(err)
	}
	result := v160ToolResult(t, ctx, conversation, "s1")
	header, output, found := strings.Cut(v160ToolResultText(result), "\nOutput:\n")
	if result.IsError || !found || !regexp.MustCompile(`^Script completed\nWall time [\d.]+ seconds$`).MatchString(header) || output != want {
		t.Fatalf("published result = %q, isError = %t; want exactly %q after the header", v160ToolResultText(result), result.IsError, want)
	}
	if calls.Load() != 2 || holds.Load() != 2 || requests.Load() != 2 {
		t.Fatalf("after reopen: globals=%d holds=%d requests=%d", calls.Load(), holds.Load(), requests.Load())
	}
}
