package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
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
		h.report(bg, &SessionFailed{Cause: cause}, ReportInfo{Type: "task_error"})
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

func TestReport140InvocationTimerCallbackAndHostOrigins(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		events := make(chan observedReport140, 8)
		definition := taskDefinition(t, "task.timer-report-140", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, err := r.SetTimer(func() { panic("callback diagnostic") }, time.Hour); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{SetTimer: func(callback func(), _ time.Duration, options TimerOptions) func() {
			if options.Background {
				return nil
			}
			callback()
			return func() { panic("cancel diagnostic") }
		}, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
			events <- observedReport140{ctx: ctx, err: err, info: info}
		}}, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		first, second := awaitReport140(t, events), awaitReport140(t, events)
		if first.info.Type != "task_error" || second.info.Type != "host_error" || second.info.Callback != "setTimer" || first.info.Task.ID != id || second.info.Task.ID != id || first.ctx != second.ctx {
			t.Fatal(first, second)
		}
		waitPublicTask(t, h, id)
	})
}
func TestReport140HostTimerFallbackAndWake(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		events := make(chan observedReport140, 8)
		var wakeCalls atomic.Int64
		h := openHarness(t, b.store, Options{SetTimer: func(func(), time.Duration, TimerOptions) func() { return nil }, OnWake: func(Wake) {
			if wakeCalls.Add(1) == 1 {
				panic("wake diagnostic")
			}
		}, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
			events <- observedReport140{ctx: ctx, err: err, info: info}
		}})
		report := awaitReport140(t, events)
		if report.info.Type != "host_error" || report.info.Callback != "onWake" || report.info.Task != nil {
			t.Fatal(report)
		}
		for i := 0; i < 2; i++ {
			cancel := h.armTimer(func() {}, time.Hour, TimerOptions{})
			if cancel == nil {
				t.Fatal("foreground fallback absent")
			}
			cancel()
		}
		report = awaitReport140(t, events)
		if report.info.Type != "host_error" || report.info.Callback != "setTimer" || report.info.Task != nil {
			t.Fatal(report)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		select {
		case extra := <-events:
			t.Fatal("duplicate timer fallback report", extra)
		default:
		}
	})
}

func TestReport140EnvironmentHostAttribution(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		cause := errors.New("private environment diagnostic")
		events := make(chan observedReport140, 4)
		definition := taskDefinition(t, "task.env-report-140", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, err := r.Environment(ctx); err == nil {
				return errors.New("environment failure lost")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		h := taskTestHarnessOptions(t, b.store, Options{Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return nil, cause }, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
			events <- observedReport140{ctx: ctx, err: err, info: info}
		}}, definition)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		report := awaitReport140(t, events)
		if report.err != cause || report.info.Type != "host_error" || report.info.Callback != "env" || report.info.Task.ID != id || report.info.Extension != "" {
			t.Fatal(report)
		}
		waitPublicTask(t, h, id)
	})
}
func TestReport140GenerationHooksAndSectionsKeepInstaller(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		events := make(chan observedReport140, 12)
		cause := errors.New("extension failure")
		extension := &Extension{Name: "original", Sections: []PromptSection{{Key: "broken", Render: func(context.Context, PromptInput) (*string, error) { return nil, cause }}}}
		extension.Hooks = GenerationHooks{BeforeRequest: func(context.Context, []MessageReceipt, *HookAPI) ([]MessageReceipt, error) {
			registry.Uninstall(extension)
			extension.Name = "changed"
			return nil, cause
		}, AfterResponse: func(context.Context, MessageReceipt, *HookAPI) error { return cause }}
		if err := registry.Install(extension); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("answer")
			close(ch)
			return ch
		})
		options.Registry = registry
		options.OnReportWithInfo = func(ctx context.Context, err error, info ReportInfo) {
			events <- observedReport140{ctx: ctx, err: err, info: info}
		}
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		if _, err := conversation.Submit(bg, Input{Content: "request"}); err != nil {
			t.Fatal(err)
		}
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 3; i++ {
			report := awaitReport140(t, events)
			if report.err != cause || report.info.Type != "extension_error" || report.info.Extension != "original" || report.info.Task == nil || report.info.Task.Kind != "pi.generation" || report.info.Task.ConversationID != conversation.ID() {
				t.Fatal(report)
			}
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := h.WaitForIdle(ctx); err != nil {
			t.Fatal(err)
		}
	})
}
func TestReport140TaskHookAndCallerOrigins(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		cause := errors.New("handler failure")
		own := errors.New("caller failure")
		events := make(chan observedReport140, 8)
		definition := taskDefinition(t, "task.custom-origin-140", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if err := r.EachHook(ctx, "ping", func(handler TaskHook) error {
				_, err := handler(ctx, nil)
				if err != cause {
					t.Error("handler error identity changed", err)
				}
				return err
			}); err != nil {
				return err
			}
			if err := r.EachHook(ctx, "ping", func(TaskHook) error { return own }); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		registry := NewRegistry()
		if _, err := registry.RegisterTask(definition); err != nil {
			t.Fatal(err)
		}
		if err := registry.Install(&Extension{Name: "handler", TaskHooks: []TaskHookRegistration{{Task: definition.Kind(), Handlers: map[string]TaskHook{"ping": func(context.Context, any) (any, error) { return nil, cause }}}}}); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, OnReportWithInfo: func(ctx context.Context, err error, info ReportInfo) {
			events <- observedReport140{ctx: ctx, err: err, info: info}
		}})
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		first, second := awaitReport140(t, events), awaitReport140(t, events)
		if first.err != cause || first.info.Type != "extension_error" || first.info.Extension != "handler" || first.info.Task.ID != id || second.err != own || second.info.Type != "task_error" || second.info.Extension != "" {
			t.Fatal(first, second)
		}
		waitPublicTask(t, h, id)
	})
}

func TestReport140ModelHostFailuresReportAfterFallbackAdoption(t *testing.T) {
	for _, kind := range []string{"resolver", "options"} {
		t.Run(kind, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				events := make(chan observedReport140, 4)
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					t.Error("request dispatched after host failure")
					return nil
				})
				if kind == "resolver" {
					options.Models = func(goai.Provider, string) *goai.Model { panic("private resolver panic") }
				} else {
					options.RequestOptions = func(context.Context, ModelRef) (*goai.StreamOptions, error) {
						return nil, errors.New("private options diagnostic")
					}
				}
				var h *Harness
				options.OnReportWithInfo = func(ctx context.Context, err error, info ReportInfo) {
					state, e := h.Snapshot(bg)
					if e != nil {
						t.Error(e)
					} else if task := state.Tasks[info.Task.ID]; !terminalStatus(task.Status) && !taskHasDecidedOutcome(task) {
						t.Error("host report preceded fallback adoption")
					}
					events <- observedReport140{ctx: ctx, err: err, info: info}
				}
				h = openHarness(t, b.store, options)
				conversation := root(t, h, ref)
				submission, err := conversation.Submit(bg, Input{Content: "host failure"})
				if err != nil {
					t.Fatal(err)
				}
				if err = h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				report := awaitReport140(t, events)
				if report.info.Type != "host_error" || report.info.Callback != "models" || report.info.Task.Kind != "pi.generation" || report.info.Task.ConversationID != conversation.ID() {
					t.Fatal(report)
				}
				waitSubmission(t, submission)
			})
		})
	}
}
