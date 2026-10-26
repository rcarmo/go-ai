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
		var histograms, counters int
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
			if name != "pi.durable.reports" || value != 1 || len(attributes) != 1 || attributes["type"] != "task_error" {
				t.Error(name, value, attributes)
			}
			mu.Lock()
			counters++
			mu.Unlock()
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
		if len(phases) != 3 || histograms != 3 || counters != 1 {
			t.Fatal(len(phases), histograms, counters)
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
