package durable

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source harness-tool-gates.test.ts:112, :137, :166 and :197. The provider and tool
// produce local test receipts; the hook, task park and signal path are real.
func TestSourceV160BeforeToolGateSleep(t *testing.T) {
	for _, mode := range []string{"approved", "denied", "expired", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 5*time.Second)
			defer cancel()
			registry := NewRegistry()
			approval, err := DefineSignal(SignalDefinition{Name: "test.approval", Version: 1, Schema: JSON{"type": "object", "properties": JSON{"allow": JSON{"type": "boolean"}}, "required": []any{"allow"}}})
			if err != nil {
				t.Fatal(err)
			}
			key, err := approval.Key("rm_-rf_build")
			if err != nil {
				t.Fatal(err)
			}
			var runs, passes, quiet, requests, clock atomic.Int64
			clock.Store(1000)
			reg := wrapRegistration("bash")
			reg.Execute = func(_ context.Context, args any, _ *ToolAPI) (ToolResult, error) {
				// This is a receipt-only stand-in, not a shell execution.
				runs.Add(1)
				command, ok := generationObject(args)["command"].(string)
				if !ok {
					return ToolResult{}, fmt.Errorf("missing stand-in argument")
				}
				return ToolResult{Content: "ran " + command}, nil
			}
			after := make(chan ToolResult, 1)
			err = registry.Install(&Extension{Name: "gates", Tools: []ToolRegistration{reg}, ToolHooks: ToolHooks{
				BeforeTool: func(ctx context.Context, _ goai.ToolCall, api *HookAPI) (*BeforeToolResult, error) {
					passes.Add(1)
					if mode == "invalid" {
						return &BeforeToolResult{Sleep: &ToolGateSleep{}}, nil
					}
					state, err := api.ReadSignal(ctx, approval, key.Key)
					if err != nil {
						return nil, err
					}
					if state == nil {
						until := int64(2000)
						return &BeforeToolResult{Sleep: &ToolGateSleep{Until: &until, Signals: []SignalKey{key}}}, nil
					}
					if state.Expired {
						return &BeforeToolResult{Block: "No approval within a day"}, nil
					}
					if generationObject(state.Value)["allow"] != true {
						return &BeforeToolResult{Block: "Not approved"}, nil
					}
					return nil, nil
				},
				AfterTool: func(_ context.Context, _ goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
					after <- result
					return nil, nil
				},
			}})
			if err != nil {
				t.Fatal(err)
			}
			if err := registry.Install(&Extension{Name: "quiet-hours", ToolHooks: ToolHooks{BeforeTool: func(context.Context, goai.ToolCall, *HookAPI) (*BeforeToolResult, error) {
				quiet.Add(1)
				return nil, nil
			}}}); err != nil {
				t.Fatal(err)
			}
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if requests.Add(1) == 1 {
					ch <- toolAnswer("c1", "bash", JSON{"command": "rm -rf build"})
				} else {
					ch <- terminal("done")
				}
				close(ch)
				return ch
			})
			options.Registry, options.Now = registry, clock.Load
			wakes := make(chan struct{}, 16)
			options.OnWake = func(Wake) {
				select {
				case wakes <- struct{}{}:
				default:
				}
			}
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, options)
			conversation := root(t, h, ref)
			watch, err := h.WatchTaskGraph(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer watch.Stop()
			slept := make(chan TaskGraphNode, 1)
			if err := watch.Start(func(_ context.Context, graph TaskGraph, _ []Operation) error {
				for _, node := range graph.Tasks {
					if node.Kind == "pi.tool" && node.State.Status == "sleeping" {
						select {
						case slept <- node:
						default:
						}
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			sub, err := conversation.Submit(ctx, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			var toolID ID
			if mode != "invalid" {
				select {
				case node := <-slept:
					toolID = node.ID
					if node.State.Phase != "call" || node.State.Until == nil || *node.State.Until != 2000 || len(node.State.Signals) != 1 || node.State.Signals[0] != key {
						t.Fatalf("gate sleep state: %+v", node.State)
					}
				case <-ctx.Done():
					t.Fatalf("tool did not park at gate: runs=%d passes=%d: %v", runs.Load(), passes.Load(), ctx.Err())
				}
				for {
					if wake, err := h.Wake(ctx); err != nil {
						t.Fatal(err)
					} else if !wake.Active {
						break
					}
					select {
					case <-wakes:
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				if runs.Load() != 0 || passes.Load() != 1 || quiet.Load() != 0 {
					t.Fatalf("gate order before wake: runs=%d, approvals=%d, quiet-hours=%d", runs.Load(), passes.Load(), quiet.Load())
				}
				if mode == "expired" {
					clock.Store(2000)
					if err := h.Resume(ctx); err != nil {
						t.Fatal(err)
					}
				} else if got, err := h.Signal(ctx, toolID, approval, key.Key, JSON{"allow": mode == "approved"}); err != nil || got != SignalDelivered {
					t.Fatalf("approval = %q, %v", got, err)
				}
			}
			if result, err := sub.Wait(ctx); err != nil || result.Record.Status != "done" {
				t.Fatalf("submission did not finish: %+v, %v", result.Record, err)
			}
			if mode == "expired" {
				if got, err := h.Signal(ctx, toolID, approval, key.Key, JSON{"allow": true}); err != nil || got != SignalClosed {
					t.Fatalf("late approval = %q, %v", got, err)
				}
			}
			wantRuns, wantPasses := int64(0), int64(2)
			wantText := map[string]string{"approved": "ran rm -rf build", "denied": "Not approved", "expired": "No approval within a day", "invalid": "cannot wait at its gates"}[mode]
			if mode == "approved" {
				wantRuns = 1
			} else if mode == "invalid" {
				wantPasses = 1
			}
			if runs.Load() != wantRuns || passes.Load() != wantPasses || quiet.Load() != wantRuns || requests.Load() != 2 {
				t.Fatalf("runs=%d passes=%d requests=%d; want %d, %d, 2", runs.Load(), passes.Load(), requests.Load(), wantRuns, wantPasses)
			}
			entries, err := conversation.Entries(ctx, EntryQuery{}, 30, nil)
			if err != nil {
				t.Fatal(err)
			}
			toolEntries := 0
			for _, entry := range entries.Items {
				for _, message := range entry.Model {
					if message.ToolCallID != "c1" {
						continue
					}
					toolEntries++
					var texts []string
					for _, block := range message.Content {
						texts = append(texts, block.Text)
					}
					got := strings.Join(texts, "|")
					if message.IsError != (mode != "approved") || !strings.Contains(got, wantText) || mode == "approved" && got != wantText {
						t.Fatalf("tool result = %q (error=%t); want %q", got, message.IsError, wantText)
					}
				}
			}
			if toolEntries != 1 || len(after) != 1 {
				t.Fatalf("tool entries %d and afterTool receipts %d; want one each", toolEntries, len(after))
			}
		})
	}
}

// Source harness-tool-gates.test.ts:214, :235, :273, :297 and :329.
func TestSourceV160BeforeToolGateRegistryWake(t *testing.T) {
	for _, mode := range []string{"registry", "snapshot-replaced", "reopen", "execute-park", "deadline"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 5*time.Second)
			defer cancel()
			registry := NewRegistry()
			answer := testSignalAnswer(t)
			key, err := answer.Key("continue")
			if err != nil {
				t.Fatal(err)
			}
			var clock, runs, passes, requests atomic.Int64
			clock.Store(1000)
			deadline := int64(2000)
			entered := make(chan ID, 1)
			rechecked := make(chan struct{}, 1)
			release := make(chan struct{})
			reg := wrapRegistration("work")
			reg.Execute = func(_ context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
				if runs.Add(1) == 1 && mode == "execute-park" {
					return ToolResult{Park: &ToolPark{Sleep: &ToolSleep{Signals: []SignalKey{key}}}}, nil
				}
				return ToolResult{Content: "ran work"}, nil
			}
			if err := registry.Install(&Extension{Name: "tool", Tools: []ToolRegistration{reg}}); err != nil {
				t.Fatal(err)
			}
			gate := &Extension{Name: "gate", ToolHooks: ToolHooks{BeforeTool: func(ctx context.Context, _ goai.ToolCall, api *HookAPI) (*BeforeToolResult, error) {
				pass := passes.Add(1)
				if pass == 2 && mode == "registry" {
					rechecked <- struct{}{}
				}
				if pass == 1 && mode == "snapshot-replaced" {
					entered <- api.TaskID()
					select {
					case <-release:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
				if mode == "snapshot-replaced" {
					if pass >= 3 {
						return nil, nil
					}
					passKey, err := answer.Key(fmt.Sprintf("k%d", pass))
					if err != nil {
						return nil, err
					}
					return &BeforeToolResult{Sleep: &ToolGateSleep{Signals: []SignalKey{passKey}}}, nil
				}
				state, err := api.ReadSignal(ctx, answer, key.Key)
				if err != nil {
					return nil, err
				}
				if state != nil && state.Expired {
					return &BeforeToolResult{Block: "No approval within a day"}, nil
				}
				return &BeforeToolResult{Sleep: &ToolGateSleep{Until: &deadline, Signals: []SignalKey{key}}}, nil
			}}}
			if mode != "execute-park" {
				if err := registry.Install(gate); err != nil {
					t.Fatal(err)
				}
			}
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				ch := make(chan goai.Event, 1)
				if requests.Add(1) == 1 {
					ch <- toolAnswer("c1", "work", JSON{})
				} else {
					ch <- terminal("done")
				}
				close(ch)
				return ch
			})
			options.Registry, options.Now, options.OnWake = registry, clock.Load, func(Wake) {}
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, options)
			cleanupTaskGates(t, release)
			conversation := root(t, h, ref)
			watch, err := h.WatchTaskGraph(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer watch.Stop()
			slept := make(chan TaskGraphNode, 4)
			if err := watch.Start(func(_ context.Context, graph TaskGraph, _ []Operation) error {
				for _, node := range graph.Tasks {
					if node.Kind == "pi.tool" && node.State.Status == "sleeping" {
						select {
						case slept <- node:
						default:
						}
					}
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			sub, err := conversation.Submit(ctx, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			var id ID
			if mode == "snapshot-replaced" {
				select {
				case id = <-entered:
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if err := registry.Install(&Extension{Name: "new-registration"}); err != nil {
					t.Fatal(err)
				}
				releaseTaskGate(release)
			secondSleep:
				for {
					select {
					case node := <-slept:
						if len(node.State.Signals) == 1 && node.State.Signals[0].Key == "k2" {
							break secondSleep
						}
					case <-ctx.Done():
						t.Fatalf("stale gate was not woken; passes=%d: %v", passes.Load(), ctx.Err())
					}
				}
				if got, err := h.Signal(ctx, id, answer, "k2", JSON{"text": "go"}); err != nil || got != SignalDelivered {
					t.Fatalf("signal: %q, %v", got, err)
				}
			} else {
				select {
				case node := <-slept:
					id = node.ID
					if phase := node.State.Phase; mode == "execute-park" && phase != "parked" || mode != "execute-park" && phase != "call" {
						t.Fatalf("sleep phase = %q", phase)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				switch mode {
				case "registry":
					if err := registry.Install(&Extension{Name: "new-registration"}); err != nil {
						t.Fatal(err)
					}
					select {
					case <-rechecked:
					case <-ctx.Done():
						t.Fatalf("gate did not recheck on registration: %v", ctx.Err())
					}
					registry.Uninstall(gate)
				case "reopen", "deadline", "execute-park":
					if mode == "execute-park" {
						if err := registry.Install(&Extension{Name: "new-registration"}); err != nil {
							t.Fatal(err)
						}
					}
					watch.Stop()
					if err := h.Close(ctx); err != nil {
						t.Fatal(err)
					}
					if mode == "reopen" {
						registry.Uninstall(gate)
					} else if mode == "deadline" {
						clock.Store(deadline)
					}
					h = openHarness(t, reopenStoreAfterHarnessClose(t, store), options)
					if wake, err := h.Wake(ctx); err != nil || wake.Active != (mode != "execute-park") {
						t.Fatalf("wake after reopen: %+v, %v; mode %s", wake, err, mode)
					}
					sub, err = h.Submission(ctx, sub.ID())
					if err != nil {
						t.Fatal(err)
					}
					if mode == "execute-park" {
						if task, found, err := h.Task(ctx, id); err != nil || !found || task.State.Status != "sleeping" || taskCheckpointMap(task.State.Checkpoint)["phase"] != "parked" || runs.Load() != 1 {
							t.Fatalf("registry/open touched execute park: %+v, %t, %v runs=%d", task, found, err, runs.Load())
						}
						if got, err := h.Signal(ctx, id, answer, key.Key, JSON{"text": "go"}); err != nil || got != SignalDelivered {
							t.Fatalf("execute signal: %q, %v", got, err)
						}
					}
				}
			}
			result, err := sub.Wait(ctx)
			if err != nil || result.Record.Status != "done" {
				t.Fatalf("gate did not settle: %+v, %v", result.Record, err)
			}
			wantRuns, wantPasses := int64(1), int64(1)
			if mode == "execute-park" {
				wantRuns, wantPasses = 2, 0
			} else if mode == "snapshot-replaced" {
				wantPasses = 3
			} else if mode == "registry" {
				wantPasses = 2
			} else if mode == "deadline" {
				wantRuns, wantPasses = 0, 2
				record, found, err := h.Task(ctx, id)
				if err != nil || !found || record.State.Outcome == nil || record.State.Outcome.Result == nil {
					t.Fatalf("deadline outcome: %+v, found %t, err %v", record, found, err)
				}
				entryID := submissionEntryID(generationObject(record.State.Outcome.Result.Value)["entryId"])
				entry, found, err := h.Entry(ctx, entryID)
				if err != nil || !found || len(entry.Model) != 1 || len(entry.Model[0].Content) != 1 || !strings.Contains(entry.Model[0].Content[0].Text, "No approval within a day") {
					t.Fatalf("deadline result: %+v, found %t, err %v", entry.Model, found, err)
				}
				if got, err := h.Signal(ctx, id, answer, key.Key, JSON{"text": "too late"}); err != nil || got != SignalClosed {
					t.Fatalf("late gate signal: %q, %v", got, err)
				}
			}
			if runs.Load() != wantRuns || passes.Load() != wantPasses || requests.Load() != 2 {
				t.Fatalf("got runs=%d passes=%d requests=%d; want %d,%d,2", runs.Load(), passes.Load(), requests.Load(), wantRuns, wantPasses)
			}
		})
	}
}
