package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

type observedReport140 struct {
	err  error
	info ReportInfo
	ctx  context.Context
}

func awaitReport140(t *testing.T, ch <-chan observedReport140) observedReport140 {
	t.Helper()
	select {
	case report := <-ch:
		return report
	case <-time.After(3 * time.Second):
		t.Fatal("report not delivered")
		return observedReport140{}
	}
}
func TestReport140RuntimeOriginsAndPinnedInstaller(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		cause := errors.New("private diagnostic")
		events := make(chan observedReport140, 8)
		var legacy atomic.Int64
		var phaseContext context.Context
		var runtime *TaskRuntime
		var extension *Extension
		definition := taskDefinition(t, "task.reporting", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			phaseContext = ctx
			runtime = r
			registry.Uninstall(extension)
			extension.Name = "mutated"
			var originalMethod func(error) error = r.Report
			if err := originalMethod(cause); err != nil {
				return err
			}
			for _, from := range []ReportFrom{{Type: "extension", Extension: "wrapper"}, {Type: "host", Callback: "models"}} {
				if err := r.ReportFrom(cause, from); err != nil {
					return err
				}
			}
			for _, from := range []ReportFrom{{}, {Type: "host", Callback: "unknown"}, {Type: "host", Callback: "models", Extension: "wrapper"}, {Type: "extension", Callback: "env"}} {
				if err := r.ReportFrom(cause, from); err == nil {
					return errors.New("invalid origin accepted")
				}
			}
			if err := r.Report(nil); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		extension = &Extension{Name: "installer", Tasks: []*TaskDefinition{definition}}
		if err := registry.Install(extension); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, OnReport: func(err error) {
			if err != cause {
				t.Error("error identity changed")
			}
			legacy.Add(1)
		}, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
			events <- observedReport140{err: err, info: copyReportInfo(info), ctx: ctx}
			info.Task.Kind = "consumer mutation"
		}})
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		for i, kind := range []string{"task_error", "extension_error", "host_error"} {
			report := awaitReport140(t, events)
			if report.err != cause || report.ctx != phaseContext || report.info.Type != kind || report.info.Task == nil || report.info.Task.ID != id || report.info.Task.ConversationID != 1 || report.info.Task.Kind != definition.Kind() {
				t.Fatal(report)
			}
			if i == 0 && report.info.Extension != "installer" || i == 1 && report.info.Extension != "wrapper" || i == 2 && (report.info.Extension != "" || report.info.Callback != "models") {
				t.Fatal(report.info)
			}
		}
		waitPublicTask(t, h, id)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		if legacy.Load() != 3 {
			t.Fatal("legacy callback count", legacy.Load())
		}
		if err := runtime.Report(cause); !errors.Is(err, ErrSealed) {
			t.Fatal("ended report accepted", err)
		}
		// The invocation context, not an unrelated background context, is delivered.
		if runtime.Context() != phaseContext {
			t.Fatal("phase context changed")
		}
	})
}
func TestReport140FaultAfterAdoptionAndCallbackIsolation(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		cause := errors.New("phase failed")
		events := make(chan observedReport140, 2)
		var legacy atomic.Int64
		var h *Harness
		definition := taskDefinition(t, "task.fault-report", func(context.Context, TaskRecord, *TaskRuntime) error { return cause })
		h = taskTestHarnessOptions(t, b.store, Options{OnReport: func(err error) {
			if err != cause {
				t.Error(err)
			}
			legacy.Add(1)
		}, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
			snapshot, e := h.Snapshot(bg)
			if e != nil {
				t.Error(e)
			} else if snapshot.Tasks[info.Task.ID].Status != "failed" {
				t.Error("report preceded adoption")
			}
			events <- observedReport140{err: err, info: info, ctx: ctx}
			panic("observer failure")
		}}, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		report := awaitReport140(t, events)
		if report.err != cause || report.info.Type != "task_faulted" || report.info.Reason != "threw" || report.info.Task.ID != id || report.ctx == nil {
			t.Fatal(report)
		}
		waitPublicTask(t, h, id)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		if legacy.Load() != 1 {
			t.Fatal("rich panic suppressed legacy callback")
		}
	})
}

func TestReport140FatalSessionContextAndDedup(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		type key struct{}
		cause := errors.New("backend failed")
		events := make(chan observedReport140, 2)
		legacy := make(chan error, 2)
		var h *Harness
		h = openHarness(t, b.store, Options{OnReport: func(err error) { legacy <- err }, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
			join, cancel := context.WithTimeout(bg, 3*time.Second)
			defer cancel()
			if _, e := h.session.Closed(join); e != nil {
				t.Error(e)
			}
			events <- observedReport140{err: err, info: info, ctx: ctx}
		}})
		h.session.store = &failingSnapshotGeneric120{h.session.store, cause}
		ctx := context.WithValue(bg, key{}, "process-local trace")
		if _, err := h.Snapshot(ctx); !errors.Is(err, cause) {
			t.Fatal(err)
		}
		h.scheduler.report(&SessionFailed{Cause: cause})
		report := awaitReport140(t, events)
		if report.err != cause || report.info.Type != "session_failed" || report.info.Task != nil || report.ctx.Value(key{}) != "process-local trace" {
			t.Fatal(report)
		}
		select {
		case err := <-legacy:
			if err != cause {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("legacy failure not delivered")
		}
		select {
		case extra := <-events:
			t.Fatal("duplicate fatal report", extra)
		default:
		}
	})
}
