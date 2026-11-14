package durable

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Selected f9a26365 harness-telemetry.test.ts:444-476 scenario. Compaction
// uses the source pi.compaction kind. The stored background flag/carrier
// and phase parent are exact.
func TestMainBuiltinTraceBackgroundCompaction(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		backend := newMainTaskTraceBackend(t)
		var requests, hooks atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			requests.Add(1)
			ch := make(chan goai.Event, 1)
			ch <- terminal("done")
			close(ch)
			return ch
		})
		modelResolver := options.Models
		options.Models = func(p goai.Provider, id string) *goai.Model {
			model := *modelResolver(p, id)
			model.ContextWindow = 1000
			model.MaxTokens = 100
			return &model
		}
		options.Telemetry = backend.root
		registry := NewRegistry()
		if err := registry.Install(&Extension{Name: "trace-background", CompactionHooks: CompactionHooks{BeforeCompact: func(context.Context, BeforeCompactInput, *HookAPI) (*CompactionDecision, error) {
			hooks.Add(1)
			return &CompactionDecision{Decline: true}, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		options.Registry = registry
		h := openHarness(t, b.store, options)
		backend.h = h
		c, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{Compaction: CompactionPolicy{Enabled: true, ReserveTokens: 300, KeepRecentTokens: 150, BackgroundTokens: 1000}}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = c.Commit(bg, func(tx *Tx) (any, error) {
			for i, text := range []string{strings.Repeat("x", 400), strings.Repeat("y", 400)} {
				message := userReceipt(text)
				if i == 1 {
					message.Role = goai.RoleAssistant
					message.StopReason = goai.StopReasonStop
				}
				value, err := dtoObject(message, tx.limits)
				if err != nil {
					return nil, err
				}
				id, err := tx.MintID()
				if err != nil {
					return nil, err
				}
				if err := tx.AppendEntry(Entry{ID: id, Conversation: c.ID(), Kind: "message", Value: value}); err != nil {
					return nil, err
				}
			}
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := c.Submit(bg, Input{Content: strings.Repeat("z", 400)})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		if err := h.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var compact Task
		for _, task := range state.Tasks {
			if task.Kind == "pi.compaction" {
				if compact.ID != 0 {
					t.Fatal("duplicate compaction")
				}
				compact = task
			}
		}
		if compact.ID == 0 {
			t.Fatal("missing background compaction")
		}
		waitPublicTask(t, h, compact.ID)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		backend.mu.Lock()
		defer backend.mu.Unlock()
		var prepare, compaction *mainTaskTraceSpan
		for _, span := range backend.spans {
			if !span.ended {
				t.Error("unsettled span", span.name)
			}
			if prepare == nil && span.attrs["task.name"] == "pi.generation" && span.attrs["phase"] == "prepare" {
				prepare = span
			}
			if compaction == nil && span.attrs["task.name"] == "pi.compaction" {
				compaction = span
			}
		}
		if prepare == nil || compaction == nil {
			t.Fatal("missing source phases", prepare, compaction)
		}
		if !reflect.DeepEqual(compaction.parent, prepare.context) {
			t.Error("compaction parent", compaction.parent, prepare.context)
		}
		record, err := CanonicalTask(compact, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		if !record.Background {
			t.Error("compaction is not background")
		}
		mainTaskTraceFields(t, record, prepare.context, nil)
		if hooks.Load() != 1 || requests.Load() != 1 {
			t.Fatal("decline/request count", hooks.Load(), requests.Load())
		}
	})
}

// Source generation.ts:434-453 and tool.ts:930-953 createTask calls inherit
// their creating phase context via transaction.ts:449-509. These native
// controls inspect the real persisted records, not only the live span tree.
func TestMainBuiltinTraceToolMetadata(t *testing.T) {
	for _, mode := range []string{"parallel", "sequential"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				backend := newMainTaskTraceBackend(t)
				registry := NewRegistry()
				var calls atomic.Int64
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "trace_leaf", Parameters: []byte(`{"type":"object"}`)}, Implementation: "trace.leaf", Version: 1, Callers: []string{"tools"}, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{Content: "leaf"}, nil }}); err != nil {
					t.Fatal(err)
				}
				if err := registry.Register(ToolRegistration{Definition: goai.Tool{Name: "trace_outer", Parameters: []byte(`{"type":"object"}`)}, Implementation: "trace.outer", Version: 1, Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (ToolResult, error) {
					return api.CallTool(ctx, "leaf", "trace_leaf", JSON{})
				}}); err != nil {
					t.Fatal(err)
				}
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					if calls.Add(1) == 1 {
						answer := toolAnswer("call-one", "trace_outer", JSON{})
						answer.Message.Content = append(answer.Message.Content, goai.ContentBlock{Type: "toolCall", ID: "call-two", Name: "trace_outer", Arguments: map[string]any{}})
						ch <- answer
					} else {
						ch <- terminal("done")
					}
					close(ch)
					return ch
				})
				options.Telemetry = backend.root
				options.Registry = registry
				h := openHarness(t, b.store, options)
				backend.h = h
				c, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{ToolExecution: mode}})
				if err != nil {
					t.Fatal(err)
				}
				sub, err := c.Submit(bg, Input{Content: "tools"})
				if err != nil {
					t.Fatal(err)
				}
				result := waitSubmission(t, sub)
				if result.Submission.Status != "done" {
					t.Fatal(result)
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				if err := h.Close(bg); err != nil {
					t.Fatal(err)
				}
				backend.mu.Lock()
				defer backend.mu.Unlock()
				tools, nested := 0, 0
				for _, task := range state.Tasks {
					if task.Kind != "pi.tool" && task.Kind != nestedToolKind {
						continue
					}
					if task.Kind == "pi.tool" {
						tools++
					} else {
						nested++
					}
					var child *mainTaskTraceSpan
					for _, span := range backend.spans {
						if span.name == "pi.durable.phase" && span.attrs["task.id"] == uint64(task.ID) {
							child = span
							break
						}
					}
					if child == nil {
						t.Error("missing child span", task.ID)
						continue
					}
					if !child.ended {
						t.Error("unsettled child span", task.ID)
					}
					var parent *mainTaskTraceSpan
					for _, span := range backend.spans {
						if reflect.DeepEqual(span.context, child.parent) {
							parent = span
							break
						}
					}
					if parent == nil || parent.attrs["task.id"] != uint64(task.Owner) {
						t.Error("wrong creating parent", task.ID, task.Owner, child.parent)
						continue
					}
					if task.Kind == "pi.tool" {
						wantPhase := "request"
						if mode == "sequential" && task.Checkpoint["callId"] == "call-two" {
							wantPhase = "tools"
						}
						if parent.attrs["phase"] != wantPhase {
							t.Error("wrong creating phase", task.ID, parent.attrs, wantPhase)
						}
					}
					mainTaskTraceFields(t, task, parent.context, nil)
				}
				if tools != 2 || nested != 2 || calls.Load() != 2 {
					t.Fatal("task/request count", tools, nested, calls.Load())
				}
			})
		})
	}
}

// Selected harness-telemetry.test.ts:479-544: a real active invocation closes,
// then reopens with a different backend retaining only its stored data carrier.
func TestMainBuiltinTraceActiveReopenParent(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var recover atomic.Bool
		reached := make(chan struct{})
		definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.trace-active-reopen", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "hold"}, nil }, Phases: map[string]TaskPhase{
			"hold": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				if !recover.Load() {
					close(reached)
					<-ctx.Done()
					return ctx.Err()
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "finish"}}, nil
				})
			},
			"finish": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			},
		}, Abort: taskAbort})
		if err != nil {
			t.Fatal(err)
		}
		backend := newMainTaskTraceBackend(t)
		var mu sync.Mutex
		var active []float64
		metrics := &goai.TelemetryMetrics{Counter: func(name string, value float64, _ map[string]any) {
			if name == "pi.durable.tasks.active" {
				mu.Lock()
				active = append(active, value)
				mu.Unlock()
			}
		}}
		backend.root.Metrics = metrics
		h := taskTestHarnessOptions(t, b.store, Options{Telemetry: backend.root}, definition)
		backend.h = h
		creator := backend.create("creator", nil, nil, nil)
		carrier := &invocationTelemetry{}
		carrier.span.Store(creator)
		var id ID
		_, err = h.commitTasks(context.WithValue(bg, telemetryContextKey{}, carrier), 1, func(tx *Tx) error {
			var e error
			id, e = tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			return e
		})
		if err != nil {
			t.Fatal(err)
		}
		creator.End()
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, reached)
		before, ok, err := h.Task(bg, id)
		if err != nil || !ok {
			t.Fatal(before, ok, err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		recover.Store(true)
		fresh := newMainTaskTraceBackend(t)
		fresh.scope += "/reopened" // A new process must not reuse an old span ID.
		fresh.root.Metrics = metrics
		fresh.root.ExtractContext = func(c goai.TelemetryCarrier) *goai.TelemetrySpan {
			if _, err := fresh.h.Snapshot(bg); err != nil {
				t.Error("extract on Session line", err)
			}
			owned := mainTaskTraceCopy(c)
			return &goai.TelemetrySpan{Context: owned, StartSpan: func(n string, a map[string]any) *goai.TelemetrySpan { return fresh.create(n, owned, a, nil) }}
		}
		next := taskTestHarnessOptions(t, b.reopen(), Options{Telemetry: fresh.root}, definition)
		fresh.h = next
		if err := next.Resume(bg); err != nil {
			t.Fatal(err)
		}
		waitPublicTask(t, next, id)
		if err := next.Close(bg); err != nil {
			t.Fatal(err)
		}
		backend.mu.Lock()
		defer backend.mu.Unlock()
		fresh.mu.Lock()
		defer fresh.mu.Unlock()
		var phases []*mainTaskTraceSpan
		for _, span := range append(append([]*mainTaskTraceSpan{}, backend.spans...), fresh.spans...) {
			if span.name == "pi.durable.phase" {
				phases = append(phases, span)
			}
		}
		var resumed []any
		for _, phase := range phases {
			resumed = append(resumed, phase.attrs["resumed"])
			if !phase.ended {
				t.Error("unsettled phase", phase.name)
			}
			if !reflect.DeepEqual(phase.parent, before.Trace) {
				t.Error("stored parent lost", phase.parent, before.Trace)
			}
		}
		if !reflect.DeepEqual(resumed, []any{false, true, false}) {
			t.Fatal("resumed", resumed)
		}
		if phases[0].attrs["outcome"] != "aborted" || phases[0].status != "ok" {
			t.Error("closed active phase", phases[0].attrs, phases[0].status)
		}
		mu.Lock()
		defer mu.Unlock()
		if !reflect.DeepEqual(active, []float64{1, -1, 1, -1}) {
			t.Error("active metrics", active)
		}
	})
}

// Native branch controls for scheduler.ts:2250-2275: terminal and parked
// decisions precede host-close/ended detection; only an undecided phase aborts.
func TestMainBuiltinTraceCloseDecisionPrecedence(t *testing.T) {
	for _, host := range []string{"live", "closing", "ended"} {
		t.Run(host, func(t *testing.T) {
			for _, state := range []string{"interrupted", "next=finish", "waiting", "sleeping", "done", "aborted", "faulted"} {
				t.Run(state, func(t *testing.T) {
					h := &Harness{}
					h.closing.Store(host == "closing")
					var outcome, status, metric string
					ended := 0
					span := &goai.TelemetrySpan{SetAttributes: func(a map[string]any) { outcome, _ = a["outcome"].(string) }, SetStatus: func(s, _, _ string) { status = s }, End: func() { ended++ }}
					root := &goai.TelemetrySpan{Metrics: &goai.TelemetryMetrics{Histogram: func(_ string, _ float64, a map[string]any, _ *goai.TelemetrySpan) { metric, _ = a["outcome"].(string) }}}
					runtime := &TaskRuntime{harness: h, telemetry: &invocationTelemetry{root: root, phase: &durablePhaseTrace{span: span, phase: "work", state: phaseTraceState{outcome: state}}}}
					runtime.ended.Store(host == "ended")
					runtime.finishPhase()
					runtime.finishPhase()
					want := state
					if host != "live" && (state == "interrupted" || state == "next=finish") {
						want = "aborted"
					}
					wantStatus := ""
					if want == "aborted" {
						wantStatus = "ok"
					}
					if want == "faulted" {
						wantStatus = "error"
					}
					if outcome != want || metric != want || status != wantStatus || ended != 1 {
						t.Fatal(outcome, metric, status, ended, "want", want, wantStatus)
					}
				})
			}
		})
	}
}
