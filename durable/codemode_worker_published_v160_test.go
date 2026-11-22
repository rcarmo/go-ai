package durable

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// From the pinned guest prelude, QuickJS engine, and worker VM limits. The
// adapter must verify this identity before any callback, not learn it from
// the first peer message.
const officialWorkerFingerprintV160 = "quickjs-wasi 3.6.2, quickjs 0.17.0, prelude 4dce1bde, deterministic 1, stack 262144, heap 33554432, budget 100000"

func officialWorkerPackageRootV160(t *testing.T) string {
	t.Helper()
	root := os.Getenv("GOAI_CODEMODE_V160_PACKAGE_ROOT")
	if root == "" {
		t.Skip("opt-in: GOAI_CODEMODE_V160_PACKAGE_ROOT containing the verified pi-codemode 1.6.0 package and quickjs-wasi 3.6.2")
	}
	for _, dependency := range []struct{ path, version string }{
		{"@earendil-works/pi-codemode", "1.6.0"}, {"quickjs-wasi", "3.6.2"},
	} {
		data, err := os.ReadFile(filepath.Join(root, "node_modules", dependency.path, "package.json"))
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata.Version != dependency.version {
			t.Fatalf("%s version = %q; want %q", dependency.path, metadata.Version, dependency.version)
		}
	}
	return root
}

// Source: packages/codemode/test/sandbox.test.ts:506–523. Run the unchanged
// guest script in the published portable guest, rather than feeding recorded
// frames to the adapter. The portable endpoint is hosted by the same test
// subprocess bridge used for the earlier official worker.
func TestSourceV160CodemodePublishedWorkerNamespacedGlobals(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	observed := make(chan int)
	sandbox := &RemoteCodemodeSandbox{
		Remote: &CodemodeWorkerRemote{
			Transport:   officialWorkerTransport120{root: root, observed: observed},
			Fingerprint: officialWorkerFingerprintV160,
		},
		Identity: officialWorkerFingerprintV160,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	checkV160CodemodeNamespacedGlobals(t, ctx, sandbox, observed, true)
}

func checkV160CodemodeNamespacedGlobals(t *testing.T, ctx context.Context, sandbox interface {
	CodemodeSandbox
	CodemodeSandboxIdentity
}, observed chan<- int, workerArgumentPresence bool) {
	t.Helper()
	const startedAt = 100
	vm, err := sandbox.Start(ctx, v160NamespacedGlobalScript, CodemodeStart{
		RuntimeIdentity: sandbox.RuntimeIdentity(), WorkerArgumentPresence: workerArgumentPresence,
		ParallelCalls: true, DrainAcknowledgements: true, StartedAt: startedAt, Seed: 7,
		MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes, InterruptBudget: DefaultCodemodeInterruptBudget,
		Globals: []CodemodeGlobalDescription{
			{Name: "models.list", Version: 1, Spread: true},
			{Name: "models.first", Version: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	var pending *CodemodeCall
	var seen []any
	delivered := 0
	for {
		event, err := vm.Next(ctx)
		if err != nil {
			t.Fatalf("sandbox event after %d deliveries: %v", delivered, err)
		}
		switch {
		case event.Call != nil:
			call := event.Call
			if pending != nil || !call.Global || call.ID != delivered+1 {
				t.Fatalf("unexpected sandbox global: %+v", call)
			}
			pending = call
			if observed != nil {
				// Only the process bridge needs a receipt before it can admit
				// the guest's following idle frame.
				select {
				case observed <- call.ID:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
		case event.Drain != nil:
			if err := vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
				t.Fatal(err)
			}
			if pending == nil {
				continue
			}
			delivered++
			reply := CodemodeDelivery{Seq: delivered, Call: *pending, Now: startedAt + int64(delivered)}
			// A global call carries its callback value inside the public
			// envelope. The callbacks receive that value, not the envelope.
			args, present := pending.Arguments["args"]
			if !present || pending.UndefinedArgument {
				t.Fatalf("sandbox global omitted its arguments: %+v", pending)
			}
			switch pending.Name {
			case "models.list":
				seen = append(seen, args)
			case "models.first":
				reply.HasValue, reply.Value = true, args
			default:
				t.Fatalf("unexpected global callback: %s", pending.Name)
			}
			if err := vm.Deliver(ctx, reply); err != nil {
				t.Fatal(err)
			}
			pending = nil
		case event.Result != nil:
			value, err := json.Marshal(event.Result.Value)
			if err != nil || !event.Result.HasValue || event.Result.Error != "" || string(value) != `[["list","first"],"a",false]` {
				t.Fatalf("sandbox result = %+v (%s), marshal error %v", event.Result, value, err)
			}
			args, err := json.Marshal(seen)
			if err != nil || pending != nil || delivered != 3 || string(args) != `[["classifier",null,3],[]]` {
				t.Fatalf("spread callbacks = %s, deliveries = %d, pending = %+v, marshal error %v", args, delivered, pending, err)
			}
			if err := vm.Close(); err != nil {
				t.Fatal(err)
			}
			return
		default:
			t.Fatalf("unexpected sandbox event: %+v", event)
		}
	}
}
