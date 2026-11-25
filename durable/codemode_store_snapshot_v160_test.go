package durable

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

type v160StoreSnapshotCase struct {
	name, source string
	initial      JSON
	wantValue    []any
	wantSet      JSON
	wantDelete   []string
}

// packages/codemode/test/sandbox.test.ts:424–452. Use the original scripts
// and snapshots in each placement, without persisting their returned patches.
func TestSourceV160CodemodeStoreSnapshots(t *testing.T) {
	for _, tc := range []v160StoreSnapshotCase{
		{
			name: "snapshot-and-patch",
			source: `const seen = load("counter");
			store("counter", seen + 1);
			store("list", [1, { a: null }]);
			store("old", undefined);
			return [seen, load("counter"), load("missing"), load("old")];`,
			initial:    JSON{"counter": 41, "old": "x"},
			wantValue:  []any{41, 42, nil, nil},
			wantSet:    JSON{"counter": 42, "list": []any{1, JSON{"a": nil}}},
			wantDelete: []string{"old"},
		},
		{
			name:      "detached-load-and-store",
			source:    `const value = load("obj"); value.a = 2; const kept = { b: 1 }; store("kept", kept); kept.b = 2; return [load("obj").a, load("kept").b];`,
			initial:   JSON{"obj": JSON{"a": 1}},
			wantValue: []any{1, 1},
			wantSet:   JSON{"kept": JSON{"b": 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("inline-engine", func(t *testing.T) { tc.checkInlineEngine(t) })
			for _, placement := range []string{"inline-sandbox", "portable", "node-worker"} {
				t.Run(placement, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
					defer cancel()
					if placement == "inline-sandbox" {
						engine, err := NewQuickJSSandbox(ctx)
						if err != nil {
							t.Fatal(err)
						}
						defer engine.Close(context.Background())
						tc.checkSandbox(t, ctx, engine, engine.RuntimeIdentity())
						return
					}
					worker, remote := newJournalWorkerSandboxV160(t)
					if placement == "node-worker" {
						const identity = "quickjs-wasi 3.6.2, quickjs 0.17.0, prelude 4dce1bde, deterministic 1, stack 524288, heap 33554432, budget none"
						process := remote.Transport.(officialWorkerTransport120)
						remote.Transport = workerTransport120(func(ctx context.Context, request CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
							return process.executeProcess(ctx, "worker-node-v160.mjs", request, exchange)
						})
						worker.Identity, remote.Fingerprint = identity, identity
					}
					tc.checkSandbox(t, ctx, worker, worker.RuntimeIdentity())
				})
			}
		})
	}
}

func (tc v160StoreSnapshotCase) checkInlineEngine(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.Default()
	if err != nil {
		t.Fatal(err)
	}
	input := make(map[string]json.RawMessage, len(tc.initial))
	for k, v := range tc.initial {
		input[k] = v160MarshalSnapshot(t, v)
	}
	before := string(v160MarshalSnapshot(t, input))
	result := engine.Execute(ctx, tc.source, quickjs.Options{Store: input})
	if !result.OK || result.Error != nil || len(result.Calls) != 0 || len(result.Output) != 0 {
		t.Fatalf("unexpected store-only script result: %+v", result)
	}
	var value any
	if err := json.Unmarshal(result.Value, &value); err != nil {
		t.Fatal(err)
	}
	set := JSON{}
	for k, raw := range result.StoreWrites.Set {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		set[k] = v
	}
	tc.checkResult(t, value, set, result.StoreWrites.Delete)
	if string(v160MarshalSnapshot(t, input)) != before {
		t.Error("raw engine mutated the caller's snapshot")
	}
}

func (tc v160StoreSnapshotCase) checkSandbox(t *testing.T, ctx context.Context, sandbox CodemodeSandbox, identity string) {
	t.Helper()
	before := string(v160MarshalSnapshot(t, tc.initial))
	vm, err := sandbox.Start(ctx, tc.source, CodemodeStart{RuntimeIdentity: identity, Store: tc.initial, ParallelCalls: true,
		DrainAcknowledgements: true, StartedAt: 100, Seed: 7, MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes,
		InterruptBudget: DefaultCodemodeInterruptBudget})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	for {
		event, err := vm.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case event.Drain != nil:
			if event.Drain.DeliveryID != 0 || event.Drain.Issued != 0 {
				t.Fatalf("unexpected store-only drain: %+v", event.Drain)
			}
			if err := vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
				t.Fatal(err)
			}
		case event.Result != nil:
			r := event.Result
			if r.Error != "" || !r.HasValue || len(r.Images) != 0 {
				t.Fatalf("unexpected store-only script result: %+v", r)
			}
			tc.checkResult(t, r.Value, r.StoreWrites, r.StoreDeletes)
			if string(v160MarshalSnapshot(t, tc.initial)) != before {
				t.Error("sandbox mutated the caller's snapshot")
			}
			return
		default:
			t.Fatalf("unexpected store-only event: %+v", event)
		}
	}
}

func (tc v160StoreSnapshotCase) checkResult(t *testing.T, value any, set JSON, deleted []string) {
	t.Helper()
	if !equalJSONValue(value, tc.wantValue) { // L439, L451
		t.Errorf("snapshot value=%v; want %v", value, tc.wantValue)
	}
	if !equalJSONValue(set, tc.wantSet) || !slices.Equal(deleted, tc.wantDelete) { // L440, L451
		t.Errorf("patch set=%v delete=%v; want set=%v delete=%v", set, deleted, tc.wantSet, tc.wantDelete)
	}
}

func v160MarshalSnapshot(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
