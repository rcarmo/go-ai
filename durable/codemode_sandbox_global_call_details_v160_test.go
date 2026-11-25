package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

const v160GlobalCallDetailsScript = `
await attach({ ref: 1 });
attach("not awaited");
return [typeof attach, typeof globalThis.attach, await tools.echo(2)];`

func checkV160StandaloneGlobalCallDetails(t *testing.T, ok bool, value json.RawMessage, calls []string, seen []json.RawMessage) {
	t.Helper()
	if !ok || string(value) != `["function","function",2]` { // L500
		t.Errorf("standalone result ok=%v value=%s; want two function names and 2", ok, value)
	}
	if len(calls) != 1 || calls[0] != "echo" { // L501
		t.Errorf("recorded calls = %+v; want echo only", calls)
	}
	got, err := json.Marshal(seen)
	if err != nil || string(got) != `[{"ref":1},"not awaited"]` { // L503
		t.Errorf("global callback arguments = %s; %v; want both in order", got, err)
	}
}

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:489–504.
// This compares the standalone host API, not Durable effect admission.
func TestSourceV160CodemodePublishedGlobalCallDetails(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "testdata/worker-global-call-details-v160.mjs", placement, v160GlobalCallDetailsScript)
			cmd.Env = append(os.Environ(), "GOAI_WORKER_PACKAGE_ROOT="+root)
			raw, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("published global call probe failed: %v\n%s", err, raw)
			}
			var got struct {
				OK    bool
				Value json.RawMessage
				Calls []string
				Seen  []json.RawMessage
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("published global call probe returned invalid JSON: %v\n%s", err, raw)
			}
			checkV160StandaloneGlobalCallDetails(t, got.OK, got.Value, got.Calls, got.Seen)
		})
	}
}

func TestV160CodemodeNativeGlobalCallDetails(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	var mu sync.Mutex
	var seen []json.RawMessage
	got := engine.Execute(ctx, v160GlobalCallDetailsScript, quickjs.Options{
		Globals: []quickjs.Global{{Name: "attach", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
			mu.Lock()
			seen = append(seen, bytes.Clone(args))
			mu.Unlock()
			return nil, nil
		}}},
		Tools: []quickjs.Tool{{Name: "echo", Execute: func(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
			return args, nil
		}}},
	})
	mu.Lock()
	observed := append([]json.RawMessage(nil), seen...)
	mu.Unlock()
	var calls []string
	for _, call := range got.Calls {
		calls = append(calls, call.Name)
	}
	checkV160StandaloneGlobalCallDetails(t, got.OK, got.Value, calls, observed)
}
