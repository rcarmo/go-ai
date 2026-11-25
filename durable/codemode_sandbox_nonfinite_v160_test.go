package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:133–142.
func TestSourceV160CodemodeNonFiniteAcrossSandboxPlacements(t *testing.T) {
	const log = `console.log("avg", 0 / 0, 1 / 0, -1 / 0, { a: 1 }); text(0 / 0);`
	const failure = "throw NaN"
	const console = `avg NaN Infinity -Infinity {"a":1}`
	t.Run("inline", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
		defer cancel()
		engine, err := quickjs.NewEngine(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer engine.Close(context.Background())
		logged := engine.Execute(ctx, log, quickjs.Options{})
		want := []quickjs.OutputItem{{Type: "console", Text: console}, {Type: "text", Text: "NaN"}}
		if !logged.OK || !reflect.DeepEqual(logged.Output, want) {
			t.Fatalf("inline output = %+v, want %+v", logged, want)
		}
		thrown := engine.Execute(ctx, failure, quickjs.Options{})
		if thrown.OK || thrown.Error == nil || thrown.Error.Kind != "script" || thrown.Error.Message != "NaN" {
			t.Fatalf("inline throw = %+v, want script error NaN", thrown)
		}
	})

	const nodeIdentity = "quickjs-wasi 3.6.2, quickjs 0.17.0, prelude 4dce1bde, deterministic 1, stack 524288, heap 33554432, budget none"
	for _, tc := range []struct{ name, launcher, identity string }{
		{"remote", "worker-remote-120.mjs", officialWorkerFingerprintV160},
		{"worker", "worker-node-v160.mjs", nodeIdentity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			process := officialWorkerTransport120{root: officialWorkerPackageRootV160(t)}
			logged, done, result := executeV160NonFiniteWorker(t, process, tc.launcher, tc.identity, log)
			want := []CodemodeWorkerOutput{{Type: "text", Text: console, Console: true}, {Type: "text", Text: "NaN"}}
			if !done.OK || !reflect.DeepEqual(logged, want) {
				t.Fatalf("worker items = %+v, done=%+v; want %+v", logged, done, want)
			}
			if result.Error != "" || result.Output != "NaN\n<console_output>\n"+console+"\n</console_output>" {
				t.Fatalf("Go output projection = %+v", result)
			}
			_, thrown, result := executeV160NonFiniteWorker(t, process, tc.launcher, tc.identity, failure)
			var encoded string
			var detail struct{ Message string }
			if thrown.Type != "done" || thrown.OK || json.Unmarshal(thrown.Error, &encoded) != nil || json.Unmarshal([]byte(encoded), &detail) != nil || detail.Message != "NaN" || result.Error != "NaN" {
				t.Fatalf("worker throw = %+v, projected=%+v; want a script failure with message NaN", thrown, result)
			}
		})
	}
}

// Observe the raw published frames while still running them through the Go
// adapter. No tools or globals are registered in this fixture.
func executeV160NonFiniteWorker(t *testing.T, process officialWorkerTransport120, launcher, identity, code string) ([]CodemodeWorkerOutput, CodemodeWorkerMessage, CodemodeResult) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var mu sync.Mutex
	var output []CodemodeWorkerOutput
	var done CodemodeWorkerMessage
	var final CodemodeResult
	remote := &CodemodeWorkerRemote{Fingerprint: identity, Transport: workerTransport120(func(ctx context.Context, req CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
		return process.executeProcess(ctx, launcher, req, func(ctx context.Context, frames []CodemodeWorkerMessage) (*CodemodeWorkerReply, error) {
			mu.Lock()
			for _, frame := range frames {
				if frame.Type == "output" && frame.Item != nil {
					output = append(output, *frame.Item)
				}
				if frame.Type == "done" {
					done = frame
				}
			}
			mu.Unlock()
			return exchange(ctx, frames)
		})
	})}
	// Both launchers use the pinned request limits. The Node worker's
	// effective VM budget is still "none", as recorded by its fingerprint.
	start := CodemodeStart{StartedAt: 1000, Seed: 42, InterruptBudget: DefaultCodemodeInterruptBudget, MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes}
	err := remote.Execute(ctx, code, start, func(_ context.Context, ev CodemodeEvent) (CodemodeRemoteReply, error) {
		switch {
		case ev.Drain != nil && ev.Result == nil && ev.Call == nil:
			return CodemodeRemoteReply{Acknowledged: ev.Drain}, nil
		case ev.Result != nil && ev.Call == nil && ev.Drain == nil:
			mu.Lock()
			final = *ev.Result
			mu.Unlock()
			return CodemodeRemoteReply{}, nil
		default:
			return CodemodeRemoteReply{}, fmt.Errorf("unexpected standalone event: %+v", ev)
		}
	}, func(ctx context.Context) (CodemodeDelivery, error) {
		<-ctx.Done()
		return CodemodeDelivery{}, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return output, done, final
}
