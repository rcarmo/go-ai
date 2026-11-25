package durable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/quickjs"
)

const v160CatchableToolErrorScript = `try {
	await tools.fail();
	return "no error";
} catch (error) {
	return { isError: error instanceof Error, message: error.message };
}`

// packages/codemode/test/sandbox.test.ts:349–368. Unlike the application
// envelope, the native raw engine forwards the omitted argument unchanged.
func TestSourceV160CodemodeInlineEngineCatchableToolError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.Default()
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	result := engine.Execute(ctx, v160CatchableToolErrorScript, quickjs.Options{Tools: []quickjs.Tool{{
		Name: "fail", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
			calls.Add(1)
			if args != nil {
				t.Errorf("raw fail argument=%s; want omission", args)
			}
			return nil, errors.New("tool exploded")
		},
	}}})
	if !result.OK || result.Error != nil {
		t.Fatalf("looked for a catchable tool error, got %+v", result)
	}
	checkV160CaughtToolErrorValue(t, result.Value)
	if len(result.Calls) != 1 || result.Calls[0].Name != "fail" || result.Calls[0].Status != "error" || calls.Load() != 1 {
		t.Errorf("calls=%+v callbacks=%d; want one fail/error", result.Calls, calls.Load())
	}
}

// Run the original omitted-argument script through the public Go harness
// in the native inline and both published placements.
func TestSourceV160CodemodeCatchableToolError(t *testing.T) {
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var sandbox CodemodeSandbox
			if placement == "inline" {
				engine, err := NewQuickJSSandbox(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer engine.Close(context.Background())
				sandbox = engine
			} else {
				worker, remote := newJournalWorkerSandboxV160(t)
				if placement == "node-worker" {
					const identity = "quickjs-wasi 3.6.2, quickjs 0.17.0, prelude 4dce1bde, deterministic 1, stack 524288, heap 33554432, budget none"
					process := remote.Transport.(officialWorkerTransport120)
					remote.Transport = workerTransport120(func(ctx context.Context, request CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
						request.Data.MemoryLimitBytes = DefaultCodemodeMemoryLimitBytes
						return process.executeProcess(ctx, "worker-node-v160.mjs", request, exchange)
					})
					worker.Identity, remote.Fingerprint = identity, identity
				}
				sandbox = worker
			}
			checkV160CatchableToolError(t, ctx, sandbox, v160CatchableToolErrorScript)
		})
	}
}

func checkV160CatchableToolError(t *testing.T, ctx context.Context, sandbox CodemodeSandbox, source string) {
	t.Helper()
	registry := NewRegistry()
	codemode, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	var failed atomic.Int32
	fail := wrapRegistration("fail")
	fail.Execute = func(_ context.Context, args any, _ *ToolAPI) (ToolResult, error) {
		failed.Add(1)
		if !equalJSONValue(args, JSON{}) {
			t.Errorf("nested fail argument = %#v; want empty object", args)
		}
		return ToolResult{}, errors.New("tool exploded")
	}
	if err := registry.Register(fail); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("catch", "codemode", JSON{"code": source})
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
	if result, err := sub.Wait(ctx); err != nil || result.Submission.Status != "done" {
		t.Fatalf("submission status=%q err=%v; want done", result.Submission.Status, err)
	}
	result := v160ToolResult(t, ctx, conversation, "catch")
	if result.IsError || len(result.Content) != 2 {
		t.Fatalf("caught tool error must finish script successfully: %+v", result)
	}
	checkV160CaughtToolErrorValue(t, []byte(result.Content[1].Text))
	calls, ok := result.Details["calls"].([]any)
	if !ok || len(calls) != 1 {
		t.Fatalf("calls=%v; want exactly one failed call", result.Details["calls"])
	}
	if call := generationObject(calls[0]); call["name"] != "fail" || call["status"] != "error" { // L367
		t.Errorf("call=%v; want fail/error", call)
	}
	if failed.Load() != 1 || requests.Load() != 2 {
		t.Errorf("nested callbacks=%d model requests=%d; want 1 and 2", failed.Load(), requests.Load())
	}
	if _, inline := sandbox.(*QuickJSSandbox); inline {
		state, err := h.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		starts := 0
		for _, doc := range state.Documents {
			if doc.Kind == codemodeRunKind {
				starts++
				start := generationObject(doc.Value["start"])
				if start["inlineOmittedToolArguments"] != true {
					t.Fatal("new inline start did not persist its argument policy")
				}
			}
		}
		if starts != 1 {
			t.Fatalf("inline starts = %d, want one", starts)
		}
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func checkV160CaughtToolErrorValue(t *testing.T, raw []byte) {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil || !reflect.DeepEqual(value, map[string]any{"isError": true, "message": "tool exploded"}) { // L366
		t.Errorf("caught value=%q err=%v; want error instance with exact tool message", raw, err)
	}
}
