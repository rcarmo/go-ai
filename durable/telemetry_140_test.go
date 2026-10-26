package durable

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"

	goai "github.com/rcarmo/go-ai"
)

type phaseObservation140 struct {
	start, end map[string]any
	events     []map[string]any
	status     string
	ended      bool
}

func TestTelemetry140NativePhaseBoundariesAndReportPrivacy(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var mu sync.Mutex
		var phases []*phaseObservation140
		var histograms, counters, activeCalls int
		var active float64
		ended := make(chan struct{}, 8)
		var clock atomic.Int64
		clock.Store(1000)
		var h *Harness
		telemetry := &goai.TelemetrySpan{Metrics: &goai.TelemetryMetrics{Histogram: func(name string, value float64, attributes map[string]any, span *goai.TelemetrySpan) {
			if name != "pi.durable.phase.duration" || value < 0 || span == nil {
				t.Error(name, value)
			}
			mu.Lock()
			histograms++
			mu.Unlock()
		}, Counter: func(name string, value float64, attributes map[string]any) {
			mu.Lock()
			defer mu.Unlock()
			if name == "pi.durable.tasks.active" {
				if (value != 1 && value != -1) || len(attributes) != 1 || attributes["task"] != "task.telemetry-140" {
					t.Error(name, value, attributes)
				}
				active += value
				activeCalls++
				if active < 0 {
					t.Error("negative active count", active)
				}
				return
			}
			if name != "pi.durable.reports" || value != 1 || len(attributes) != 1 || attributes["type"] != "task_error" {
				t.Error(name, value, attributes)
			}
			counters++
		}}}
		telemetry.StartSpan = func(name string, attrs map[string]any) *goai.TelemetrySpan {
			if name != "pi.durable.phase" {
				t.Error(name)
			}
			if _, err := h.Snapshot(bg); err != nil {
				t.Error("backend runs on line", err)
			}
			observation := &phaseObservation140{start: attrs}
			mu.Lock()
			phases = append(phases, observation)
			mu.Unlock()
			return &goai.TelemetrySpan{Value: attrs["phase"], SetAttributes: func(attrs map[string]any) { mu.Lock(); observation.end = attrs; mu.Unlock() }, AddEvent: func(name string, attrs map[string]any) {
				if name != "pi.durable.report" {
					t.Error(name)
				}
				mu.Lock()
				observation.events = append(observation.events, attrs)
				mu.Unlock()
			}, SetStatus: func(status, _, _ string) { mu.Lock(); observation.status = status; mu.Unlock() }, End: func() { mu.Lock(); observation.ended = true; mu.Unlock(); ended <- struct{}{} }}
		}
		definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.telemetry-140", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "a"}, nil }, Abort: taskAbort, Phases: map[string]TaskPhase{
			"a": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				if span := TelemetryFromContext(ctx); span == nil || span.Value != "a" {
					t.Error("phase context missing", span)
				}
				if err := r.Report(errors.New("private transcript credentials content")); err != nil {
					return err
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "b"}}, nil
				})
			},
			"b": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.SleepUntil(ctx, 2000, JSON{"phase": "c"})
			},
			"c": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("private result"), nil })
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		h = taskTestHarnessOptions(t, b.store, Options{Telemetry: telemetry, Now: clock.Load}, definition)
		id := createPublicTask(t, h, definition, "private input", TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, ended)
		awaitTaskSignal(t, ended)
		clock.Store(2000)
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		waitPublicTask(t, h, id)
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(phases) != 3 || histograms != 3 || counters != 1 || active != 0 || activeCalls != 4 {
			t.Fatal(len(phases), histograms, counters, active, activeCalls)
		}
		for i, outcome := range []string{"next=b", "sleeping", "done"} {
			phase := phases[i]
			if !phase.ended || phase.end["outcome"] != outcome || phase.start["resumed"] != false || phase.start["kind"] != "run" || phase.start["task.id"] != uint64(id) {
				t.Fatal(i, phase)
			}
		}
		if len(phases[0].events) != 1 || phases[0].events[0]["type"] != "task_error" {
			t.Fatal(phases[0].events)
		}
		// Serialize the observed payloads, not the unexported test bookkeeping.
		payloads := make([]any, 0, len(phases))
		for _, phase := range phases {
			payloads = append(payloads, []any{phase.start, phase.end, phase.events})
		}
		data, err := json.Marshal(payloads)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "private") {
			t.Fatal("content entered telemetry", string(data))
		}
	})
}

func TestTelemetry140BackendPanicsCannotReplaceTaskOutcome(t *testing.T) {
	for _, where := range []string{"start", "attributes", "event", "status", "end", "histogram", "counter"} {
		t.Run(where, func(t *testing.T) {
			panicAt := func(name string) {
				if name == where {
					panic("backend failure")
				}
			}
			telemetry := &goai.TelemetrySpan{StartSpan: func(string, map[string]any) *goai.TelemetrySpan {
				panicAt("start")
				return &goai.TelemetrySpan{SetAttributes: func(map[string]any) { panicAt("attributes") }, AddEvent: func(string, map[string]any) { panicAt("event") }, SetStatus: func(string, string, string) { panicAt("status") }, End: func() { panicAt("end") }}
			}, Metrics: &goai.TelemetryMetrics{Histogram: func(string, float64, map[string]any, *goai.TelemetrySpan) { panicAt("histogram") }, Counter: func(string, float64, map[string]any) { panicAt("counter") }}}
			definition := taskDefinition(t, "task.panic-telemetry-140", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				if err := r.Report(errors.New("diagnostic")); err != nil {
					return err
				}
				if where == "status" {
					return errors.New(strings.Repeat("界", 600))
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			h := taskTestHarnessOptions(t, mustMemory(t), Options{Telemetry: telemetry}, definition)
			id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			result := waitPublicTask(t, h, id)
			want := "completed"
			if where == "status" {
				want = "faulted"
			}
			if result.State.Outcome == nil || result.State.Outcome.Status != want {
				t.Fatal(result)
			}
			ctx, cancel := context.WithTimeout(bg, 3*time.Second)
			defer cancel()
			if err := h.Close(ctx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTelemetry140ReopenResumesOnlyFirstPhase(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var clock atomic.Int64
		clock.Store(1000)
		sleeping := make(chan struct{})
		definition, err := DefineTask(TaskDefinitionOptions{Kind: "task.resume-telemetry-140", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "sleep"}, nil }, Abort: taskAbort, Phases: map[string]TaskPhase{
			"sleep": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				err := r.SleepUntil(ctx, 2000, JSON{"phase": "resume", "attempt": 2})
				close(sleeping)
				return err
			},
			"resume": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
					return &TaskState{Status: "running", Checkpoint: JSON{"phase": "finish"}}, nil
				})
			},
			"finish": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarnessOptions(t, b.store, Options{Now: clock.Load}, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, sleeping)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		var mu sync.Mutex
		var starts []map[string]any
		var ends int
		telemetry := &goai.TelemetrySpan{StartSpan: func(name string, attrs map[string]any) *goai.TelemetrySpan {
			mu.Lock()
			starts = append(starts, attrs)
			mu.Unlock()
			return &goai.TelemetrySpan{End: func() { mu.Lock(); ends++; mu.Unlock() }}
		}}
		clock.Store(2000)
		h = taskTestHarnessOptions(t, b.reopen(), Options{Now: clock.Load, Telemetry: telemetry}, definition)
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		waitPublicTask(t, h, id)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(starts) != 2 || ends != 2 {
			t.Fatal(starts, ends)
		}
		if starts[0]["resumed"] != true || starts[0]["attempt"] != int64(2) || starts[1]["resumed"] != false {
			t.Fatal(starts)
		}
	})
}

func TestTelemetry140DiagnosticBoundAndDisabled(t *testing.T) {
	if TelemetryFromContext(nil) != nil || TelemetryFromContext(bg) != nil {
		t.Fatal("unexpected span")
	}
	for _, text := range []string{"short", strings.Repeat("界", 600), strings.Repeat("😀", 300), strings.Repeat("a", 499) + "😀tail"} {
		got := telemetryDiagnostic(text)
		if !strings.HasPrefix(text, got) || len(utf16.Encode([]rune(got))) > 500 {
			t.Fatal("invalid diagnostic bound", got)
		}
		if len(utf16.Encode([]rune(text))) <= 500 && got != text {
			t.Fatal("short diagnostic changed")
		}
	}
}

func TestTelemetry140SubmitAdmissionDedupAndPrivacy(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var h *Harness
		var starts, ends []map[string]any
		var statuses []string
		var ended int
		telemetry := &goai.TelemetrySpan{StartSpan: func(name string, attrs map[string]any) *goai.TelemetrySpan {
			if name != "pi.durable.submit" {
				t.Error(name)
			}
			if !h.mu.TryLock() {
				t.Error("start callback under Harness lock")
			} else {
				h.mu.Unlock()
			}
			if _, err := h.Snapshot(bg); err != nil {
				t.Error(err)
			}
			starts = append(starts, attrs)
			return &goai.TelemetrySpan{SetAttributes: func(attrs map[string]any) {
				if !h.mu.TryLock() {
					t.Error("end callback under Harness lock")
				} else {
					h.mu.Unlock()
				}
				if _, err := h.Snapshot(bg); err != nil {
					t.Error(err)
				}
				ends = append(ends, attrs)
			}, SetStatus: func(status, code, message string) {
				statuses = append(statuses, status)
				if code != "" || message != "" {
					t.Error("content-bearing admission diagnostic", code, message)
				}
			}, End: func() { ended++ }}
		}}
		h = openHarness(t, b.store, Options{Telemetry: telemetry})
		conv, err := h.Root(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		input := Input{Type: "write", Content: "private transcript content", RequestID: "private request ID"}
		first, err := conv.Submit(bg, input)
		if err != nil {
			t.Fatal(err)
		}
		second, err := conv.Submit(bg, input)
		if err != nil {
			t.Fatal(err)
		}
		if first.id != second.id {
			t.Fatal("dedup changed ID")
		}
		if _, err := conv.Submit(bg, Input{RequestID: input.RequestID, Content: "private conflict"}); err == nil {
			t.Fatal("cross-type accepted")
		}
		if _, err := conv.Submit(bg, Input{WhenBusy: "private invalid"}); err == nil {
			t.Fatal("invalid busy accepted")
		}
		if len(starts) != 4 || len(ends) != 2 || ended != 4 || len(statuses) != 2 {
			t.Fatal(starts, ends, statuses, ended)
		}
		for _, attrs := range ends {
			if attrs["submission.id"] != uint64(first.id) || attrs["result"] != "done" {
				t.Fatal(attrs)
			}
		}
		for i, attrs := range starts {
			kind := "input"
			if i < 2 {
				kind = "write"
			}
			if attrs["submission.type"] != kind || attrs["conversation.id"] != uint64(conv.id) {
				t.Fatal(attrs)
			}
		}
		data, err := json.Marshal([]any{starts, ends, statuses})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "private") {
			t.Fatal("content in submit span", string(data))
		}
	})
}

func TestTelemetry140SubmitBackendPanicsAndCallerParent(t *testing.T) {
	for _, where := range []string{"start", "attributes", "status", "end"} {
		t.Run(where, func(t *testing.T) {
			panicAt := func(name string) {
				if name == where {
					panic("telemetry failure")
				}
			}
			var rootStarts, callerStarts int
			telemetry := &goai.TelemetrySpan{StartSpan: func(string, map[string]any) *goai.TelemetrySpan {
				rootStarts++
				panicAt("start")
				return &goai.TelemetrySpan{SetAttributes: func(map[string]any) { panicAt("attributes") }, SetStatus: func(string, string, string) { panicAt("status") }, End: func() { panicAt("end") }}
			}}
			h := openHarness(t, mustMemory(t), Options{Telemetry: telemetry})
			conv, err := h.Root(bg, AgentChange{})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := conv.Submit(bg, Input{Type: "write", Content: "hello"}); err != nil {
				t.Fatal(err)
			}
			if _, err := conv.Submit(bg, Input{Type: "invalid"}); err == nil {
				t.Fatal("invalid accepted")
			}
			carrier := &invocationTelemetry{}
			carrier.span.Store(&goai.TelemetrySpan{StartSpan: func(string, map[string]any) *goai.TelemetrySpan { callerStarts++; return &goai.TelemetrySpan{} }})
			ctx := context.WithValue(bg, telemetryContextKey{}, carrier)
			if _, err := conv.Submit(ctx, Input{Type: "write", Content: "child"}); err != nil {
				t.Fatal(err)
			}
			if _, err := conv.Submit(ctx, Input{Type: "invalid"}); err == nil {
				t.Fatal("invalid accepted")
			}
			if rootStarts != 3 || callerStarts != 1 {
				t.Fatal(rootStarts, callerStarts)
			}
		})
	}
}

func TestTelemetry140SubmitInputUsesCommittedAdmissionResult(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var mu sync.Mutex
		var results []string
		ref, opts := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("private answer")
			close(ch)
			return ch
		})
		opts.Telemetry = &goai.TelemetrySpan{StartSpan: func(name string, attrs map[string]any) *goai.TelemetrySpan {
			if name != "pi.durable.submit" {
				return nil
			}
			if attrs["submission.type"] != "input" {
				t.Error(attrs)
			}
			return &goai.TelemetrySpan{SetAttributes: func(attrs map[string]any) {
				mu.Lock()
				defer mu.Unlock()
				result, _ := attrs["result"].(string)
				results = append(results, result)
			}}
		}}
		h := openHarness(t, b.store, opts)
		conv := root(t, h, ref)
		sub, err := conv.Submit(bg, Input{Content: "private input", RequestID: "private request"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		again, err := conv.Submit(bg, Input{Content: "private changed input", RequestID: "private request"})
		if err != nil {
			t.Fatal(err)
		}
		if again.id != sub.id {
			t.Fatal("dedup changed admission")
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(results) != 2 || results[0] != "placed" || results[1] != "done" {
			t.Fatal(results)
		}
	})
}

func TestTelemetry140InputRootsLinkCallersAndWritesRemainChildren(t *testing.T) {
	var roots, children int
	caller := &goai.TelemetrySpan{StartSpan: func(name string, attrs map[string]any) *goai.TelemetrySpan { children++; return &goai.TelemetrySpan{} }}
	carrier := &invocationTelemetry{}
	carrier.span.Store(caller)
	ctx := context.WithValue(bg, telemetryContextKey{}, carrier)
	telemetry := &goai.TelemetrySpan{StartRootSpan: func(name string, attrs map[string]any, links []*goai.TelemetrySpan) *goai.TelemetrySpan {
		roots++
		if name != "pi.durable.submit" || attrs["submission.type"] != "input" || len(links) != 1 || links[0] != caller {
			t.Error(name, attrs, links)
		}
		return &goai.TelemetrySpan{}
	}}
	h := openHarness(t, mustMemory(t), Options{Telemetry: telemetry})
	conv, err := h.Root(bg, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	// Invalid inputs still cover their complete public admission without exposing error text.
	if _, err := conv.Submit(ctx, Input{Type: "invalid"}); err == nil {
		t.Fatal("invalid accepted")
	}
	if _, err := conv.Submit(ctx, Input{Type: "write", Content: "child"}); err != nil {
		t.Fatal(err)
	}
	if roots != 1 || children != 1 {
		t.Fatal(roots, children)
	}
	trace := conv.beginSubmitTrace(ctx, Input{})
	submitCtx := context.WithValue(ctx, submitTelemetryContextKey{}, trace)
	if TelemetryFromContext(submitCtx) != trace.span {
		t.Fatal("submit did not replace caller scope")
	}
	trace.finish(nil, errors.New("reject"))
	// The new backend operation is isolated just like existing callbacks.
	h.options.Telemetry = &goai.TelemetrySpan{StartRootSpan: func(string, map[string]any, []*goai.TelemetrySpan) *goai.TelemetrySpan { panic("backend failure") }}
	if _, err := conv.Submit(ctx, Input{Type: "invalid"}); err == nil {
		t.Fatal("invalid accepted after panic")
	}
}

func TestTelemetry140NestedTasksInheritCreatingPhaseAndReleaseParents(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var mu sync.Mutex
		ancestry := map[string]string{}
		var backend func(string) *goai.TelemetrySpan
		backend = func(parent string) *goai.TelemetrySpan {
			return &goai.TelemetrySpan{StartSpan: func(name string, attrs map[string]any) *goai.TelemetrySpan {
				task, _ := attrs["task.name"].(string)
				mu.Lock()
				ancestry[task] = parent
				mu.Unlock()
				return backend(task)
			}}
		}
		child := taskDefinition(t, "task.child-carrier-140", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		parent, err := DefineTask(TaskDefinitionOptions{Kind: "task.parent-carrier-140", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "spawn"}, nil }, Abort: taskAbort, Phases: map[string]TaskPhase{
			"spawn": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				// Binding, not context propagation alone, identifies the creating phase.
				return r.Commit(bg, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					id, err := tx.CreateTask(child, nil, TaskOptions{Ownership: TaskOwnership{Kind: "task", Task: r.TaskID()}})
					if err != nil {
						return nil, err
					}
					return &TaskState{Status: "waiting", Checkpoint: JSON{"phase": "join"}, On: []ID{id}, Policy: "failFast"}, nil
				})
			},
			"join": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarnessOptions(t, b.store, Options{Telemetry: backend("root")}, parent, child)
		id := createPublicTask(t, h, parent, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		result := waitPublicTask(t, h, id)
		if result.State.Outcome == nil || result.State.Outcome.Status != "completed" {
			t.Fatal(result)
		}
		if err := h.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		if err := h.session.readTasks(bg, func(Snapshot) error {
			if len(h.telemetryParents) != 0 {
				t.Error("terminal ancestry retained", len(h.telemetryParents))
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if ancestry["task.parent-carrier-140"] != "root" || ancestry["task.child-carrier-140"] != "task.parent-carrier-140" {
			t.Fatal(ancestry)
		}
	})
}

func TestTelemetry140ActiveInvocationBalancedOnClose(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var h *Harness
		var mu sync.Mutex
		var values []float64
		entered := make(chan struct{})
		telemetry := &goai.TelemetrySpan{Metrics: &goai.TelemetryMetrics{Counter: func(name string, value float64, attrs map[string]any) {
			if name != "pi.durable.tasks.active" {
				return
			}
			if len(attrs) != 1 || attrs["task"] != "task.active-close-140" {
				t.Error(attrs)
			}
			if _, err := h.Snapshot(bg); err != nil {
				t.Error("metric on mutation line", err)
			}
			mu.Lock()
			values = append(values, value)
			mu.Unlock()
		}}}
		definition := taskDefinition(t, "task.active-close-140", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
		h = taskTestHarnessOptions(t, b.store, Options{Telemetry: telemetry}, definition)
		createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		mu.Lock()
		if len(values) != 1 || values[0] != 1 {
			t.Error(values)
		}
		mu.Unlock()
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := h.Close(ctx); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(values) != 2 || values[0] != 1 || values[1] != -1 {
			t.Fatal("Close did not balance active counts", values)
		}
	})
}
