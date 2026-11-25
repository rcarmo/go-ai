package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

// Source: packages/codemode/test/sandbox.test.ts:206–222.
const v160ExitScript = `text("before");
store("k", 1);
await tools.echo(1);
try { exit(); } catch {}
text("after");
return "unreachable";`

func TestSourceV160CodemodeInlineEngineExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.Default()
	if err != nil {
		t.Fatal(err)
	}
	seen := make(chan string, 4)
	result := engine.Execute(ctx, v160ExitScript, quickjs.Options{Tools: []quickjs.Tool{{
		Name: "echo", Execute: func(ctx context.Context, args json.RawMessage) (json.RawMessage, error) {
			select {
			case seen <- string(args):
				return args, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}}})
	if !result.OK || result.Error != nil || result.Value != nil || !reflect.DeepEqual(result.Output, []quickjs.OutputItem{{Type: "text", Text: "before"}}) ||
		len(result.StoreWrites.Set) != 1 || string(result.StoreWrites.Set["k"]) != "1" || len(result.StoreWrites.Delete) != 0 {
		t.Fatalf("exit result = %+v; want success, undefined, only before, and store set k=1", result)
	}
	if len(result.Calls) != 1 || result.Calls[0].Name != "echo" || result.Calls[0].Status != "ok" || len(seen) != 1 || <-seen != "1" {
		t.Fatalf("calls = %+v; want one successful echo with numeric argument 1", result.Calls)
	}
}

// The raw sandbox API permits echo(1). Durable's application tool envelope
// requires object arguments, so use the published wire *before* the Go
// application adapter for the exact scalar. Keep the placements separate.
func TestSourceV160CodemodePublishedExit(t *testing.T) {
	const nodeWorkerIdentity = "quickjs-wasi 3.6.2, quickjs 0.17.0, prelude 4dce1bde, deterministic 1, stack 524288, heap 33554432, budget none"
	for _, tc := range []struct{ name, launch, fingerprint string }{
		{"portable", "worker-remote-120.mjs", officialWorkerFingerprintV160},
		{"node-worker", "worker-node-v160.mjs", nodeWorkerIdentity},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := officialWorkerPackageRootV160(t)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			observed := make(chan int)
			transport := officialWorkerTransport120{root: root, observed: observed}
			request := CodemodeWorkerRequest{Data: CodemodeWorkerStart{Code: v160ExitScript,
				Tools: []CodemodeWorkerTool{{Name: "echo", JSName: "echo"}}, Globals: []CodemodeWorkerGlobal{}, Store: map[string]string{},
				Deterministic: &CodemodeWorkerDeterminism{StartedAt: 100, Seed: 7}, MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes},
				InterruptBudget: DefaultCodemodeInterruptBudget}
			var mu sync.Mutex
			var outputs []CodemodeWorkerOutput
			var arguments []string
			var terminal CodemodeWorkerMessage
			var started, done int
			err := transport.executeProcess(ctx, tc.launch, request, func(ctx context.Context, batch []CodemodeWorkerMessage) (*CodemodeWorkerReply, error) {
				mu.Lock()
				defer mu.Unlock()
				var reply *CodemodeWorkerReply
				for _, message := range batch {
					switch message.Type {
					case "started":
						started++
						if started != 1 || message.Fingerprint != tc.fingerprint {
							return nil, fmt.Errorf("guest start %d fingerprint = %q; want %q", started, message.Fingerprint, tc.fingerprint)
						}
					case "output":
						if message.Item == nil || done != 0 {
							return nil, fmt.Errorf("invalid guest output event: %+v", message)
						}
						outputs = append(outputs, *message.Item)
					case "call":
						if started != 1 || done != 0 || len(arguments) != 0 || message.ID != 1 || message.Target != "tool" || message.Name != "echo" || message.Args == nil || *message.Args != "1" {
							return nil, fmt.Errorf("unexpected guest call: %+v", message)
						}
						arguments = append(arguments, *message.Args)
						select {
						case observed <- message.ID:
						case <-ctx.Done():
							return nil, ctx.Err()
						}
						reply = &CodemodeWorkerReply{Type: "result", ID: 1, OK: true, Payload: message.Args, Seq: 1, Now: 101}
					case "idle":
					case "done":
						done++
						terminal = message
					default:
						return nil, fmt.Errorf("unexpected guest message: %+v", message)
					}
				}
				return reply, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if done != 1 || !terminal.OK || terminal.Value != nil || len(terminal.Error) != 0 || terminal.Writes == nil || *terminal.Writes != `[["k","1"]]` ||
				!reflect.DeepEqual(outputs, []CodemodeWorkerOutput{{Type: "text", Text: "before"}}) {
				t.Errorf("exit terminal = %+v, output=%+v; want success, undefined, only before, and store set k=1", terminal, outputs)
			}
			if !reflect.DeepEqual(arguments, []string{"1"}) || terminal.During == nil || *terminal.During != (CodemodeWorkerDuring{Seq: 1, Issued: 1}) {
				t.Errorf("echo args=%v terminal position=%+v; want one completed numeric echo", arguments, terminal.During)
			}
		})
	}
}

// Supplemental public Go application-boundary check: replace only the
// scalar argument with the corresponding allowed object.
func TestV160CodemodeSandboxExitObjectArgument(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	t.Run("inline", func(t *testing.T) {
		sandbox, err := NewQuickJSSandbox(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer sandbox.Close(context.Background())
		checkV160ExitObject(t, ctx, sandbox, sandbox.RuntimeIdentity())
	})
	t.Run("portable-adapter", func(t *testing.T) {
		sandbox, _ := newJournalWorkerSandboxV160(t)
		checkV160ExitObject(t, ctx, sandbox, sandbox.RuntimeIdentity())
	})
}

func checkV160ExitObject(t *testing.T, ctx context.Context, sandbox CodemodeSandbox, identity string) {
	t.Helper()
	vm, err := sandbox.Start(ctx, strings.Replace(v160ExitScript, "echo(1)", "echo({n:1})", 1), CodemodeStart{
		RuntimeIdentity: identity, ParallelCalls: true, DrainAcknowledgements: true, StartedAt: 100, Seed: 7,
		Tools: []CodemodeToolDescription{{Name: "echo", Identifier: "echo"}}, MemoryLimitBytes: DefaultCodemodeMemoryLimitBytes,
		InterruptBudget: DefaultCodemodeInterruptBudget,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()
	var pending *CodemodeCall
	delivered := false
	for {
		event, err := vm.Next(ctx)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case event.Call != nil:
			if delivered || pending != nil || event.Call.ID != 1 || event.Call.Global || event.Call.Name != "echo" || !equalJSONValue(event.Call.Arguments, JSON{"n": 1}) {
				t.Fatalf("unexpected object echo call: %+v", event.Call)
			}
			pending = event.Call
		case event.Drain != nil:
			if err := vm.(CodemodeDrainExecution).AcknowledgeDrain(ctx, *event.Drain); err != nil {
				t.Fatal(err)
			}
			if pending != nil {
				if err := vm.Deliver(ctx, CodemodeDelivery{Seq: 1, Now: 101, Call: *pending, Value: pending.Arguments, HasValue: true}); err != nil {
					t.Fatal(err)
				}
				pending, delivered = nil, true
			}
		case event.Result != nil:
			r := event.Result
			if !delivered || r.Error != "" || r.HasValue || r.Value != nil || r.Output != "before" || len(r.Images) != 0 ||
				!equalJSONValue(r.StoreWrites, JSON{"k": 1}) || len(r.StoreDeletes) != 0 {
				t.Errorf("exit result = %+v, delivered=%t; want success, undefined, before, and store set k=1", r, delivered)
			}
			return
		default:
			t.Fatalf("unexpected sandbox event: %+v", event)
		}
	}
}
