package durable

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Official v1.5.0 f9a26365 harness-after-run.test.ts assertions and native
// controls, mapped per function below. The original source181/251 assertions
// are promoted from the saved failure baseline without weakening them.
type mainAfterRunProbe struct {
	aborted atomic.Int64
	mu      sync.Mutex
	asked   []RunSummary
	runs    []string
	started chan struct{}
	release chan struct{}
}

func mainAfterRunTask(t *testing.T, probe *mainAfterRunProbe) *TaskDefinition {
	t.Helper()
	def, err := DefineTask(TaskDefinitionOptions{Kind: "task.after-run-tests", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil },
		Phases: map[string]TaskPhase{"run": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			label := task.Input.Value.(map[string]any)["label"].(string)
			probe.mu.Lock()
			probe.runs = append(probe.runs, label)
			probe.mu.Unlock()
			close(probe.started)
			if probe.release != nil {
				select {
				case <-probe.release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				id, err := tx.mintID()
				if err != nil {
					return nil, err
				}
				err = tx.appendEntryRecord(Entry{ID: id, Conversation: r.ConversationID(), Kind: "test.tests", Value: JSON{"label": label}, ByTask: r.TaskID()})
				return taskDone(JSON{"label": label}), err
			})
		}}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			probe.aborted.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
			})
		}})
	if err != nil {
		t.Fatal(err)
	}
	return def
}

func mainAfterRunLive(t *testing.T, h *Harness, conversation ID) JSON {
	t.Helper()
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	for _, doc := range state.Documents {
		if !doc.Retired && doc.Kind == "pi.live" && doc.Owner == conversation {
			return doc.Value
		}
	}
	t.Fatal("missing live document")
	return nil
}

func TestMainAfterRunAnswerTasksHoldRun(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		probe := &mainAfterRunProbe{started: make(chan struct{}), release: make(chan struct{})}
		def := mainAfterRunTask(t, probe)
		registry := NewRegistry()
		if err := registry.Register(wrapRegistration("edit")); err != nil {
			t.Fatal(err)
		}
		err := registry.Install(&Extension{Name: "after-run", Tasks: []*TaskDefinition{def}, Hooks: GenerationHooks{
			AfterRun: func(ctx context.Context, run RunSummary, api *HookAPI) (*AfterRunResult, error) {
				// The callback must see the actual committed answer, off the line.
				entry, ok, err := api.Entry(ctx, run.Answer)
				if err != nil || !ok || entry.ID != run.Answer {
					t.Error("hook ran before committed answer", entry, ok, err)
				}
				probe.mu.Lock()
				probe.asked = append(probe.asked, run)
				probe.mu.Unlock()
				return &AfterRunResult{Tasks: []RunEndTask{{Task: def, Input: JSON{"label": "run 1"}}}}, nil
			},
		}})
		if err != nil {
			t.Fatal(err)
		}
		var requests atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				ch <- toolAnswer("a", "edit", JSON{})
			} else {
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, probe.release)
		c := root(t, h, ref)
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		settlement := waitSubmission(t, sub)
		if settlement.Record.Status != "done" {
			t.Fatal("input not settled at answer", settlement)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		idle := make(chan error, 1)
		go func() { idle <- c.WaitForIdle(ctx) }()
		select {
		case <-probe.started:
		case err := <-idle:
			t.Fatal("run ended before requested afterRun task started", err)
		case <-ctx.Done():
			t.Fatal("afterRun task did not start", ctx.Err())
		}
		live := mainAfterRunLive(t, h, c.ID())
		run := generationObject(live["run"])
		inputs, ok := run["inputs"].([]any)
		if !ok || len(inputs) != 0 {
			t.Error("answered inputs retained", run)
		}
		after := generationObject(run["afterRun"])
		tasks, ok := after["tasks"].([]any)
		if !ok || len(tasks) != 1 {
			t.Fatal("requested child list", run)
		}
		child, ok, err := h.Task(bg, submissionEntryID(tasks[0]))
		if err != nil || !ok || child.Owner != submissionEntryID(run["taskId"]) || child.Background {
			t.Fatal("afterRun ownership/background", child, ok, err)
		}
		select {
		case err := <-idle:
			t.Fatal("run became idle with held afterRun task", err)
		default:
		}
		entries, err := c.Entries(bg, EntryQuery{Order: Ascending}, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		var first, answer ID
		for _, entry := range entries.Items {
			if !isMessageEntry(entry) {
				continue
			}
			var receipt MessageReceipt
			if err := decodeEntryMessage(entry, h.session.limits, &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Role == goai.RoleUser && first == 0 {
				first = entry.ID
			}
			if receipt.Role == goai.RoleAssistant {
				answer = entry.ID
			}
		}
		probe.mu.Lock()
		asked := append([]RunSummary(nil), probe.asked...)
		probe.mu.Unlock()
		if first == 0 || answer == 0 || !reflect.DeepEqual(asked, []RunSummary{{Reason: "answer", First: first, Answer: answer, Last: answer}}) {
			t.Error("run summary", asked, first, answer)
		}
		releaseTaskGate(probe.release)
		if err := <-idle; err != nil {
			t.Fatal(err)
		}
		live = mainAfterRunLive(t, h, c.ID())
		if live["run"] != nil || live["runFailure"] != nil {
			t.Error("run/failure survived drain", live)
		}
		entries, err = c.Entries(bg, EntryQuery{Order: Ascending}, 100, nil)
		if err != nil || entries.Items[len(entries.Items)-1].Kind != "test.tests" {
			t.Fatal("child result is not the last entry", entries, err)
		}
		generation, ok, err := h.Task(bg, submissionEntryID(run["taskId"]))
		if err != nil || !ok || generation.State.Outcome == nil || generation.State.Outcome.Status != "completed" || generation.State.Outcome.Result == nil {
			t.Fatal("generation outcome", generation, ok, err)
		}
		value, _ := generation.State.Outcome.Result.Value.(map[string]any)
		if submissionEntryID(value["entryId"]) != answer || requests.Load() != 2 {
			t.Error("answer result/request count", value, answer, requests.Load())
		}
	})
}

// Split source251's no-hook and empty-hook setups into independent functions,
// so the unchanged runtime's valid no-hook control is not hidden by a failure.
func TestMainAfterRunHookReadsCommittedEntryBoundaries(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var h *Harness
		registry := NewRegistry()
		if err := registry.Install(&Extension{Name: "after-run-history", Hooks: GenerationHooks{AfterRun: func(ctx context.Context, run RunSummary, api *HookAPI) (*AfterRunResult, error) {
			if run.First == 0 || run.Answer == 0 || run.Last == 0 {
				t.Error("run summary missing entry ids", run)
				return nil, nil
			}
			for _, id := range []ID{run.First, run.Answer, run.Last} {
				entry, ok, err := api.Entry(ctx, id)
				if err != nil || !ok || entry.ID != id {
					t.Errorf("hook cannot read committed entry %d: ok=%v err=%v entry=%+v", id, ok, err, entry)
				}
			}
			first, ok, err := api.Entry(ctx, run.First)
			if err != nil || !ok {
				t.Errorf("hook cannot read first entry %d: ok=%v err=%v", run.First, ok, err)
				return nil, nil
			}
			var user MessageReceipt
			if err := decodeEntryMessage(first, h.session.limits, &user); err != nil || user.Role != goai.RoleUser {
				t.Errorf("run first entry must remain user-visible: receipt=%+v kind=%q err=%v", user, first.Kind, err)
			}
			answer, ok, err := api.Entry(ctx, run.Answer)
			if err != nil || !ok {
				t.Errorf("hook cannot read answer entry %d: ok=%v err=%v", run.Answer, ok, err)
				return nil, nil
			}
			var assistant MessageReceipt
			if err := decodeEntryMessage(answer, h.session.limits, &assistant); err != nil || assistant.Role != goai.RoleAssistant {
				t.Errorf("run answer entry must remain assistant-visible: receipt=%+v kind=%q err=%v", assistant, answer.Kind, err)
			}
			return nil, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("done")
			close(ch)
			return ch
		})
		options.Registry = registry
		h = openHarness(t, b.store, options)
		c := root(t, h, ref)
		if _, err := c.Submit(bg, Input{Content: "go"}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := c.WaitForIdle(ctx); err != nil {
			t.Fatal(err)
		}
		if live := mainAfterRunLive(t, h, c.ID()); len(live) != 0 {
			t.Error("run did not clear after hook-visible read", live)
		}
	})
}

func TestMainAfterRunContinueAfterFailureSummaryUsesFailedFirst(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var h *Harness
		probe := &mainAfterRunProbe{started: make(chan struct{})}
		registry := NewRegistry()
		if err := registry.Install(&Extension{Name: "after-run-continue-history", Hooks: GenerationHooks{AfterRun: func(_ context.Context, run RunSummary, _ *HookAPI) (*AfterRunResult, error) {
			probe.mu.Lock()
			defer probe.mu.Unlock()
			probe.asked = append(probe.asked, run)
			return nil, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			switch calls.Add(1) {
			case 1:
				message := terminal("").Message
				message.Content = nil
				message.StopReason, message.ErrorMessage = goai.StopReasonError, "Invalid request"
				ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: message}
			case 2:
				ch <- terminal("recovered")
			default:
				t.Error("unexpected provider request after continue")
				ch <- terminal("unexpected")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h = openHarness(t, b.store, options)
		c := root(t, h, ref)
		firstSub, err := c.Submit(bg, Input{Content: "Analyze this"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, firstSub)
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var first ID
		for _, entry := range state.Entries {
			if entry.Conversation != c.ID() || !isMessageEntry(entry) {
				continue
			}
			var receipt MessageReceipt
			if err := decodeEntryMessage(entry, h.session.limits, &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Role == goai.RoleUser {
				first = entry.ID
				break
			}
		}
		if first == 0 {
			t.Fatal("failed run missing original user entry")
		}
		resumed, err := c.Submit(bg, Input{Type: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		settled := waitSubmission(t, resumed)
		if settled.Record.Type != "continue" || settled.Record.Status != "done" || settled.Record.Answer == 0 {
			t.Fatalf("continue settlement = %+v; want continue/done with answer", settled.Record)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := c.WaitForIdle(ctx); err != nil {
			t.Fatal(err)
		}
		probe.mu.Lock()
		defer probe.mu.Unlock()
		if len(probe.asked) != 1 {
			t.Fatalf("afterRun hook count = %d; want 1", len(probe.asked))
		}
		want := RunSummary{Reason: "answer", First: first, Answer: settled.Record.Answer, Last: settled.Record.Answer}
		if !reflect.DeepEqual(probe.asked[0], want) {
			t.Errorf("continue summary = %+v; want %+v", probe.asked[0], want)
		}
	})
}

func TestMainAfterRunNoHookImmediateEnd(t *testing.T) {
	mainAfterRunEmpty(t, false)
}
func TestMainAfterRunEmptyHookEnd(t *testing.T) {
	mainAfterRunEmpty(t, true)
}
func mainAfterRunEmpty(t *testing.T, hook bool) {
	t.Helper()
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		probe := &mainAfterRunProbe{started: make(chan struct{})}
		def := mainAfterRunTask(t, probe)
		hooks := GenerationHooks{}
		if hook {
			hooks.AfterRun = func(_ context.Context, run RunSummary, _ *HookAPI) (*AfterRunResult, error) {
				probe.mu.Lock()
				probe.asked = append(probe.asked, run)
				probe.mu.Unlock()
				return nil, nil
			}
		}
		registry := NewRegistry()
		if err := registry.Install(&Extension{Name: "after-run-empty", Tasks: []*TaskDefinition{def}, Hooks: hooks}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("done")
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		if hook {
			ctx, cancel := context.WithTimeout(bg, 3*time.Second)
			defer cancel()
			if err := c.WaitForIdle(ctx); err != nil {
				t.Fatal(err)
			}
		}
		live := mainAfterRunLive(t, h, c.ID())
		if len(live) != 0 {
			t.Error("live document not empty at run end", live)
		}
		probe.mu.Lock()
		defer probe.mu.Unlock()
		want := 0
		if hook {
			want = 1
		}
		if len(probe.asked) != want || len(probe.runs) != 0 {
			t.Fatal("afterRun dispatch/task counts", len(probe.asked), len(probe.runs), "want", want, 0)
		}
	})
}

// Source538 selected representable requests: unknown/builtin/non-JSON/throwing/
// bad-start/valid, then a throwing hook. A non-list Tasks value cannot inhabit
// the typed Go slice and receives no runtime/body-parity credit.
func TestMainAfterRunRejectedRequestsKeepValidSibling(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		probe := &mainAfterRunProbe{started: make(chan struct{})}
		valid := mainAfterRunTask(t, probe)
		unknown := taskDefinition(t, "test.unknown", func(context.Context, TaskRecord, *TaskRuntime) error { return nil })
		throwing, err := DefineTask(TaskDefinitionOptions{Kind: "test.throwing", Version: 1, Initial: func(any) (JSON, error) { return nil, errors.New("initial failed") }, Phases: map[string]TaskPhase{"run": func(context.Context, TaskRecord, *TaskRuntime) error { return nil }}, Abort: taskAbort})
		if err != nil {
			t.Fatal(err)
		}
		bad, err := DefineTask(TaskDefinitionOptions{Kind: "test.bad-start", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "nowhere"}, nil }, Phases: map[string]TaskPhase{"run": func(context.Context, TaskRecord, *TaskRuntime) error { return nil }}, Abort: taskAbort})
		if err != nil {
			t.Fatal(err)
		}
		builtinToken := &TaskDefinition{constructed: true, options: TaskDefinitionOptions{Kind: "pi.generation"}}
		registry := NewRegistry()
		if err := registry.Install(&Extension{Name: "after-run", Tasks: []*TaskDefinition{valid, throwing, bad}, Hooks: GenerationHooks{AfterRun: func(context.Context, RunSummary, *HookAPI) (*AfterRunResult, error) {
			return &AfterRunResult{Tasks: []RunEndTask{{Task: unknown}, {Task: builtinToken, Input: JSON{}}, {Task: valid, Input: JSON{"label": func() string { return "not JSON" }}}, {Task: throwing}, {Task: bad}, {Task: valid, Input: JSON{"label": "ok"}}}}, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		if err := registry.Install(&Extension{Name: "broken", Hooks: GenerationHooks{AfterRun: func(context.Context, RunSummary, *HookAPI) (*AfterRunResult, error) {
			return nil, errors.New("hook failed")
		}}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("done")
			close(ch)
			return ch
		})
		options.Registry = registry
		var mu sync.Mutex
		var reports []error
		var extensions []string
		options.OnReportWithInfo = func(_ context.Context, e error, info ReportInfo) {
			mu.Lock()
			defer mu.Unlock()
			if info.Type != "extension_error" {
				t.Error("unexpected report", info, e)
			}
			reports = append(reports, e)
			extensions = append(extensions, info.Extension)
		}
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := c.WaitForIdle(ctx); err != nil {
			t.Fatal(err)
		}
		probe.mu.Lock()
		runs := append([]string(nil), probe.runs...)
		probe.mu.Unlock()
		if !reflect.DeepEqual(runs, []string{"ok"}) {
			t.Error("valid sibling lost", runs)
		}
		mu.Lock()
		defer mu.Unlock()
		if !reflect.DeepEqual(extensions, []string{"after-run", "broken"}) {
			t.Fatal("per-hook reports", extensions)
		}
		var aggregate interface{ Unwrap() []error }
		if !errors.As(reports[0], &aggregate) {
			t.Fatal("request errors not aggregated", reports[0])
		}
		var messages []string
		for _, e := range aggregate.Unwrap() {
			messages = append(messages, e.Error())
		}
		want := []string{"Task test.unknown is not installed", "A hook cannot start the built-in task pi.generation", "durable: rejected: unsupported JSON value", "initial failed", "Task test.bad-start starts at the unknown phase nowhere"}
		if !reflect.DeepEqual(messages, want) {
			t.Error("ordered request diagnostics", messages, want)
		}
		if reports[1].Error() != "hook failed" {
			t.Error("hook cause", reports[1])
		}
		if live := mainAfterRunLive(t, h, c.ID()); len(live) != 0 {
			t.Error("run did not end", live)
		}
	})
}

// Source617: the installed version supplies both checkpoint and runtime; the
// request token is only a kind reference. Initialisation must be off the line.
func TestMainAfterRunInstalledDefinitionInitialOnce(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var installed, token atomic.Int64
		var h *Harness
		makeDefinition := func(version uint64, count *atomic.Int64) *TaskDefinition {
			d, err := DefineTask(TaskDefinitionOptions{Kind: "test.versioned", Version: version, Initial: func(any) (JSON, error) {
				count.Add(1)
				if _, err := h.Snapshot(bg); err != nil {
					t.Error("initializer on Session line", err)
				}
				return JSON{"phase": "run"}, nil
			}, Phases: map[string]TaskPhase{"run": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			}}, Abort: taskAbort})
			if err != nil {
				t.Fatal(err)
			}
			return d
		}
		current, stale := makeDefinition(2, &installed), makeDefinition(1, &token)
		registry := NewRegistry()
		if err := registry.Install(&Extension{Name: "tasks", Tasks: []*TaskDefinition{current}}); err != nil {
			t.Fatal(err)
		}
		if err := registry.Install(&Extension{Name: "asker", Hooks: GenerationHooks{AfterRun: func(context.Context, RunSummary, *HookAPI) (*AfterRunResult, error) {
			return &AfterRunResult{Tasks: []RunEndTask{{Task: stale}}}, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("done")
			close(ch)
			return ch
		})
		options.Registry = registry
		h = openHarness(t, b.store, options)
		c := root(t, h, ref)
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := c.WaitForIdle(ctx); err != nil {
			t.Fatal(err)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var tasks []TaskRecord
		for _, task := range state.Tasks {
			if task.Conversation == c.ID() && task.Kind == "test.versioned" {
				record, err := CanonicalTask(task, h.session.limits)
				if err != nil {
					t.Fatal(err)
				}
				tasks = append(tasks, record)
			}
		}
		if len(tasks) != 1 {
			t.Error("requested task count", tasks)
		} else if tasks[0].Version != 2 || tasks[0].State.Status != "terminal" {
			t.Error("installed version/outcome", tasks[0])
		}
		if installed.Load() != 1 || token.Load() != 0 {
			t.Error("initial calls", installed.Load(), token.Load())
		}
	})
}

// Source221: terminate's answer is its tool-calling assistant; last is the
// newest result, or the reset appended by handoff, never a synthetic answer.
func TestMainAfterRunTerminateSummary(t *testing.T) {
	for _, mode := range []string{"finish", "handoff"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				probe := &mainAfterRunProbe{started: make(chan struct{})}
				def := mainAfterRunTask(t, probe)
				registry := NewRegistry()
				registration := wrapRegistration(mode)
				registration.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
					control := &ToolControl{Terminate: true}
					if mode == "handoff" {
						s := "fresh start"
						control.Handoff = &s
					}
					return ToolResult{Content: "finished", Control: control}, nil
				}
				if err := registry.Register(registration); err != nil {
					t.Fatal(err)
				}
				if err := registry.Install(&Extension{Name: "after-run", Tasks: []*TaskDefinition{def}, Hooks: GenerationHooks{AfterRun: func(_ context.Context, run RunSummary, _ *HookAPI) (*AfterRunResult, error) {
					probe.mu.Lock()
					probe.asked = append(probe.asked, run)
					probe.mu.Unlock()
					return &AfterRunResult{Tasks: []RunEndTask{{Task: def, Input: JSON{"label": "run 1"}}}}, nil
				}}}); err != nil {
					t.Fatal(err)
				}
				var requests atomic.Int64
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					requests.Add(1)
					ch := make(chan goai.Event, 1)
					ch <- toolAnswer("a", mode, JSON{})
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				c := root(t, h, ref)
				sub, err := c.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				settlement := waitSubmission(t, sub)
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				if err := c.WaitForIdle(ctx); err != nil {
					t.Fatal(err)
				}
				entries, err := c.Entries(bg, EntryQuery{Order: Ascending}, 100, nil)
				if err != nil {
					t.Fatal(err)
				}
				var last ID
				for _, entry := range entries.Items {
					if mode == "handoff" {
						if entry.Kind == "pi.reset" {
							last = entry.ID
						}
					} else if isMessageEntry(entry) {
						var m MessageReceipt
						if err := decodeEntryMessage(entry, h.session.limits, &m); err != nil {
							t.Fatal(err)
						}
						if m.Role == goai.RoleToolResult {
							last = entry.ID
						}
					}
				}
				probe.mu.Lock()
				defer probe.mu.Unlock()
				want := []RunSummary{{Reason: "terminate", First: settlement.Record.Entry, Answer: settlement.Record.Answer, Last: last}}
				if last == 0 || !reflect.DeepEqual(probe.asked, want) || !reflect.DeepEqual(probe.runs, []string{"run 1"}) {
					t.Error("terminate summary/tasks", probe.asked, probe.runs, want)
				}
				if live := mainAfterRunLive(t, h, c.ID()); live["run"] != nil {
					t.Error("terminate run remained", live)
				}
				if requests.Load() != 1 {
					t.Error("terminate redispatched model", requests.Load())
				}
			})
		})
	}
}

// Source478: Stop during either hooks or children ends an already answered run
// completed, with no runFailure. Actual native abort marks/signals/joins apply.
func TestMainAfterRunStopAnswered(t *testing.T) {
	for _, stage := range []string{"hook", "task"} {
		t.Run(stage, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				probe := &mainAfterRunProbe{started: make(chan struct{}), release: make(chan struct{})}
				def := mainAfterRunTask(t, probe)
				hookStarted := make(chan struct{})
				registry := NewRegistry()
				if err := registry.Install(&Extension{Name: "after-run", Tasks: []*TaskDefinition{def}, Hooks: GenerationHooks{AfterRun: func(ctx context.Context, _ RunSummary, _ *HookAPI) (*AfterRunResult, error) {
					close(hookStarted)
					if stage == "hook" {
						<-ctx.Done()
						return nil, ctx.Err()
					}
					return &AfterRunResult{Tasks: []RunEndTask{{Task: def, Input: JSON{"label": "run"}}}}, nil
				}}}); err != nil {
					t.Fatal(err)
				}
				ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
					ch := make(chan goai.Event, 1)
					ch <- terminal("done")
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				cleanupTaskGates(t, probe.release)
				c := root(t, h, ref)
				sub, err := c.Submit(bg, Input{Content: "go"})
				if err != nil {
					t.Fatal(err)
				}
				settlement := waitSubmission(t, sub)
				awaitTaskSignal(t, hookStarted)
				if stage == "task" {
					awaitTaskSignal(t, probe.started)
				}
				beforeStop := mainAfterRunLive(t, h, c.ID())
				if err := c.Abort(bg); err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				if err := c.WaitForIdle(ctx); err != nil {
					t.Fatal(err)
				}
				if live := mainAfterRunLive(t, h, c.ID()); live["run"] != nil || live["runFailure"] != nil {
					t.Error("answered Stop failure/live", live)
				}
				// Source generation.abort returns aborted even after answering; the
				// input stays done and no failure is published (source478).
				generationID := submissionEntryID(generationObject(beforeStop["run"])["taskId"])
				task, ok, err := h.Task(bg, generationID)
				if err != nil || !ok || task.State.Outcome == nil || task.State.Outcome.Status != "aborted" {
					t.Error("answered Stop outcome", task, ok, err)
				}
				status, err := sub.Status(bg)
				if err != nil || status.Status != "done" || status.Answer != settlement.Record.Answer {
					t.Error("answered input changed", status, err)
				}
				wantRuns := []string{}
				wantAborted := int64(0)
				if stage == "task" {
					wantRuns, wantAborted = []string{"run"}, 1
				}
				probe.mu.Lock()
				if !reflect.DeepEqual(append([]string{}, probe.runs...), wantRuns) || probe.aborted.Load() != wantAborted {
					t.Error("Stop task runs/aborts", probe.runs, probe.aborted.Load())
				}
				probe.mu.Unlock()
			})
		})
	}
}

// Native orderly-close control for source1140's finish resumption. This does
// not claim its initial crash/held-progress stream assertions.
func TestMainAfterRunCloseInFinishReasks(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		probe := &mainAfterRunProbe{started: make(chan struct{})}
		def := mainAfterRunTask(t, probe)
		entered := make(chan struct{})
		registry := NewRegistry()
		var asks, requests atomic.Int64
		if err := registry.Install(&Extension{Name: "after-run", Tasks: []*TaskDefinition{def}, Hooks: GenerationHooks{AfterRun: func(ctx context.Context, run RunSummary, _ *HookAPI) (*AfterRunResult, error) {
			probe.mu.Lock()
			probe.asked = append(probe.asked, run)
			probe.mu.Unlock()
			if asks.Add(1) == 1 {
				close(entered)
				<-ctx.Done()
			}
			return &AfterRunResult{Tasks: []RunEndTask{{Task: def, Input: JSON{"label": "run"}}}}, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			requests.Add(1)
			ch := make(chan goai.Event, 1)
			ch <- terminal("done")
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		settlement := waitSubmission(t, sub)
		awaitTaskSignal(t, entered)
		before := mainAfterRunLive(t, h, c.ID())
		if before["generation"] != nil || !reflect.DeepEqual(generationObject(generationObject(before["run"])["afterRun"]), JSON{}) {
			t.Fatal("commit A", before)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		store := b.reopen()
		h2 := openHarness(t, store, options)
		c2, err := h2.Conversation(bg, c.ID())
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := c2.WaitForIdle(ctx); err != nil {
			t.Fatal(err)
		}
		probe.mu.Lock()
		defer probe.mu.Unlock()
		if len(probe.asked) != 2 || probe.asked[0] != probe.asked[1] || !reflect.DeepEqual(probe.runs, []string{"run"}) {
			t.Error("finish replay", probe.asked, probe.runs)
		}
		if asks.Load() != 2 || requests.Load() != 1 {
			t.Error("repeated model or hook", asks.Load(), requests.Load())
		}
		if live := mainAfterRunLive(t, h2, c.ID()); len(live) != 0 {
			t.Error("reopened run", live)
		}
		resumed, err := h2.Submission(bg, sub.ID())
		if err != nil {
			t.Fatal(err)
		}
		status, err := resumed.Status(bg)
		if err != nil || status.Status != "done" || status.Answer != settlement.Record.Answer {
			t.Fatal("reopened answer changed", status, err)
		}
		generationID := submissionEntryID(generationObject(before["run"])["taskId"])
		task, ok, err := h2.Task(bg, generationID)
		if err != nil || !ok || task.State.Outcome == nil || task.State.Outcome.Status != "completed" {
			t.Error("resumed outcome", task, ok, err)
		}
	})
}

// Native queued-boundary control for selected source278 assertions. Go has no
// per-input whenIdle=reject, and this external write is not the source child's
// same-transaction submit; neither missing assertion is credited.
func TestMainAfterRunQueuedBoundary(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		probe := &mainAfterRunProbe{started: make(chan struct{}), release: make(chan struct{})}
		def := mainAfterRunTask(t, probe)
		registry := NewRegistry()
		var asks, requests atomic.Int64
		if err := registry.Install(&Extension{Name: "after-run", Tasks: []*TaskDefinition{def}, Hooks: GenerationHooks{AfterRun: func(context.Context, RunSummary, *HookAPI) (*AfterRunResult, error) {
			if asks.Add(1) != 1 {
				return nil, nil
			}
			return &AfterRunResult{Tasks: []RunEndTask{{Task: def, Input: JSON{"label": "run 1"}}}}, nil
		}}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			switch requests.Add(1) {
			case 1:
				ch <- terminal("first")
			case 2:
				ch <- terminal("second")
			case 3:
				ch <- terminal("third")
			default:
				t.Error("extra generation")
				ch <- terminal("unexpected")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, probe.release)
		c := root(t, h, ref)
		sub, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		awaitTaskSignal(t, probe.started)
		steer, err := c.Submit(bg, Input{Content: "steer", WhenBusy: "steer"})
		if err != nil {
			t.Fatal(err)
		}
		follow, err := c.Submit(bg, Input{Content: "later"})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Submit(bg, Input{Content: "x", WhenBusy: "reject"}); err == nil {
			t.Error("busy input accepted")
		}
		if _, err := c.Submit(bg, Input{Type: "continue"}); err == nil {
			t.Error("busy continue accepted")
		}
		steer2, err := c.Submit(bg, Input{Content: "steer 2", WhenBusy: "steer"})
		if err != nil {
			t.Fatal(err)
		}
		note, err := c.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "test.note", Value: JSON{}}})
		if err != nil {
			t.Fatal(err)
		}
		if status, err := note.Status(bg); err != nil || status.Status != "done" {
			t.Error("note not immediately settled", status, err)
		}
		write, err := c.Submit(bg, Input{Type: "write", Entry: &Entry{Kind: "test.result", Value: JSON{}, Model: []MessageReceipt{userReceipt("tests passed")}}})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []*SubmissionHandle{steer, follow, steer2, write} {
			record, err := s.Status(bg)
			if err != nil || terminalStatus(record.Status) {
				t.Error("queued item settled during tasks", record, err)
			}
		}
		releaseTaskGate(probe.release)
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := c.WaitForIdle(ctx); err != nil {
			t.Fatal(err)
		}
		for _, s := range []*SubmissionHandle{steer, follow, steer2, write} {
			record, err := s.Status(bg)
			if err != nil || record.Status != "done" {
				t.Error("queued item not settled", record, err)
			}
		}
		entries, err := c.Entries(bg, EntryQuery{Order: Ascending}, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, entry := range entries.Items {
			switch entry.Kind {
			case "test.tests", "test.result", "test.note":
				got = append(got, entry.Kind)
			case userEntryKind, assistantEntryKind, toolResultEntryKind:
				var m MessageReceipt
				if err := decodeEntryMessage(entry, h.session.limits, &m); err != nil {
					t.Fatal(err)
				}
				text := ""
				if m.ContentText != nil {
					text = *m.ContentText
				}
				for _, block := range m.Content {
					if block.Type == "text" {
						text += block.Text
					}
				}
				got = append(got, text)
			}
		}
		want := []string{"go", "first", "test.note", "test.tests", "test.result", "steer", "later", "second", "steer 2", "third"}
		if !reflect.DeepEqual(got, want) {
			t.Error("afterRun boundary order", got, want)
		}
		if asks.Load() != 3 || requests.Load() != 3 {
			t.Error("run/hook count", asks.Load(), requests.Load())
		}
	})
}

// Native name adaptation must reject mapped builtins without reserving a
// previously valid application task namespace wholesale.
func TestMainAfterRunBuiltinNames(t *testing.T) {
	for _, name := range []string{"pi.generation", "pi.tool", "pi.application", "pi.compaction", nestedToolKind, "task.pi.subagent-reporter"} {
		if !afterRunBuiltinKind(name) {
			t.Error("builtin admitted", name)
		}
	}
	for _, name := range []string{"test.versioned", "task.pi.application", "task.piano", "app.task"} {
		if afterRunBuiltinKind(name) {
			t.Error("application namespace rejected", name)
		}
	}
}

// Source: harness-hook-tasks.test.ts, afterTools tasks / onYield tasks.
func TestSourceV160GenerationHookDecisionTasks(t *testing.T) {
	for _, mode := range []string{"terminate", "steer", "invalid-after-tools", "continue", "invalid-yield", "stand", "queued", "after-run", "abort-yield"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 8*time.Second)
			defer cancel()
			afterTools := mode == "terminate" || mode == "steer" || mode == "invalid-after-tools"
			var calls, asked atomic.Int32
			var answer ID
			requests := make(chan []string, 4)
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, c *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				var users []string
				for _, message := range c.Messages {
					if message.Role == goai.RoleUser {
						for _, b := range message.Content {
							if b.Type == "text" {
								users = append(users, b.Text)
							}
						}
					}
				}
				requests <- users
				ch := make(chan goai.Event, 1)
				if calls.Add(1) == 1 {
					if afterTools {
						ch <- toolAnswer("c1", "noop", JSON{})
					} else {
						ch <- terminal("first")
					}
				} else {
					ch <- terminal("second")
				}
				close(ch)
				return ch
			})
			entered := make(chan TaskRecord, 1)
			release := make(chan struct{})
			child, err := DefineTask(TaskDefinitionOptions{Kind: "test.decider", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "decide"}, nil },
				Phases: map[string]TaskPhase{"decide": func(run context.Context, task TaskRecord, r *TaskRuntime) error {
					entered <- task
					select {
					case <-release:
					case <-run.Done():
						return run.Err()
					}
					var result any
					switch mode {
					case "terminate":
						result = JSON{"terminate": true}
					case "continue", "queued":
						result = JSON{"continue": "review: add a test"}
					case "invalid-after-tools":
						result = JSON{"continue": "x"}
					case "invalid-yield":
						result = JSON{"continue": []any{JSON{"type": "text"}}}
					}
					conversation, err := r.Conversation(run, r.ConversationID())
					if err != nil {
						return err
					}
					return r.Commit(run, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
						if mode == "steer" {
							if _, err := conversation.SubmitIn(tx, Input{Type: "input", Content: "also check the tests", WhenBusy: "steer"}); err != nil {
								return nil, err
							}
						}
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: result}}}, nil
					})
				}}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
						return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
					})
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			taskRequest := func() *HookTasksResult {
				if asked.Add(1) != 1 {
					return nil
				}
				return &HookTasksResult{Tasks: []RunEndTask{{Task: child}}}
			}
			hooks := GenerationHooks{}
			if afterTools {
				hooks.AfterToolsTasks = func(_ context.Context, assistant ID, results []ID, _ *HookAPI) (*HookTasksResult, error) {
					if assistant == 0 || len(results) != 1 || results[0] <= assistant {
						t.Errorf("afterTools entries: %d %v", assistant, results)
					}
					answer = assistant
					return taskRequest(), nil
				}
			} else {
				hooks.OnYieldTasks = func(context.Context, MessageReceipt, *HookAPI) (*HookTasksResult, error) { return taskRequest(), nil }
			}
			afterRun := make(chan any, 1)
			if mode == "after-run" {
				hooks.AfterRun = func(ctx context.Context, _ RunSummary, api *HookAPI) (*AfterRunResult, error) {
					value, _, err := api.SnapshotDefinition(ctx, mountedDocumentDefinitions[1], api.ConversationID(), nil)
					if err != nil {
						return nil, err
					}
					marker := generationObject(generationObject(value.Copy())["run"])["yield"]
					afterRun <- marker
					return nil, nil
				}
			}
			options.Registry = NewRegistry()
			if err := options.Registry.Install(&Extension{Name: "deciders", Tools: []ToolRegistration{wrapRegistration("noop")}, Tasks: []*TaskDefinition{child}, Hooks: hooks}); err != nil {
				t.Fatal(err)
			}
			infos := make(chan ReportInfo, 16)
			options.OnReportWithInfo = func(_ context.Context, _ error, info ReportInfo) { infos <- info }
			native, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, native, options)
			c := root(t, h, ref)
			sub, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			type result struct {
				settlement Settlement
				err        error
			}
			settled := make(chan result, 1)
			go func() { s, err := sub.Wait(ctx); settled <- result{s, err} }()
			var task TaskRecord
			select {
			case task = <-entered:
			case got := <-settled:
				t.Fatalf("run settled before starting the hook task: %+v, %v", got.settlement.Record, got.err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			parent, found, err := h.Task(ctx, task.Owner)
			phase := "yield"
			if afterTools {
				phase = "afterTools"
			}
			if err != nil || !found || parent.Version != 3 || parent.Kind != "pi.generation" || parent.State.Status != "waiting" || parent.State.Policy != "allSettled" || !reflect.DeepEqual(parent.State.On, []ID{task.ID}) || taskCheckpointMap(parent.State.Checkpoint)["phase"] != phase {
				t.Fatalf("generation wait = %+v; child = %+v; %v", parent, task, err)
			}
			if !afterTools {
				answer = submissionEntryID(taskCheckpointMap(parent.State.Checkpoint)["answer"])
				state, err := h.Snapshot(ctx)
				if err != nil {
					t.Fatal(err)
				}
				live := mainContinueBoundaryDocument(t, state, c.ID(), "pi.live")
				marker := generationObject(generationObject(live["run"])["yield"])
				if answer == 0 || len(marker) != 2 || submissionEntryID(marker["answer"]) != answer || !reflect.DeepEqual(generationTaskIDs(marker["tasks"]), []ID{task.ID}) {
					t.Fatalf("yield marker = %v, answer=%d", marker, answer)
				}
				if pending, err := sub.Status(ctx); err != nil || pending.Status == "done" || pending.Answer != 0 {
					t.Fatalf("yield settled its input early: %+v, %v", pending, err)
				}
			}
			var queued *SubmissionHandle
			if mode == "queued" {
				queued, err = c.Submit(ctx, Input{Type: "input", Content: "a question", WhenBusy: "followUp"})
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "abort-yield" {
				if err := c.Abort(ctx); err != nil {
					t.Fatal(err)
				}
			} else {
				close(release)
			}
			got := <-settled
			if got.err != nil || got.settlement.Record.Status != "done" {
				t.Fatalf("submission: %+v, %v", got.settlement.Record, got.err)
			}
			switch mode {
			case "terminate", "invalid-yield", "abort-yield", "stand", "after-run":
				if calls.Load() != 1 || got.settlement.Record.Answer != answer {
					t.Fatalf("expected first answer %d, got %+v after %d requests", answer, got.settlement.Record, calls.Load())
				}
			case "queued":
				later, err := queued.Wait(ctx)
				if err != nil || later.Record.Status != "done" || got.settlement.Record.Answer != answer || later.Record.Answer <= answer || calls.Load() != 2 {
					t.Fatalf("queued boundary: first=%+v, next=%+v requests=%d err=%v", got.settlement.Record, later.Record, calls.Load(), err)
				}
				<-requests
				if users := <-requests; !reflect.DeepEqual(users, []string{"go", "a question"}) {
					t.Fatalf("queued request users = %#v", users)
				}
			case "continue", "steer":
				if calls.Load() != 2 || got.settlement.Record.Answer <= answer {
					t.Fatalf("expected second answer, got %+v after %d requests", got.settlement.Record, calls.Load())
				}
				want := "review: add a test"
				if mode == "steer" {
					want = "also check the tests"
				}
				<-requests
				if users := <-requests; !reflect.DeepEqual(users, []string{"go", want}) {
					t.Fatalf("second request users = %#v", users)
				}
			case "invalid-after-tools":
				if calls.Load() != 2 {
					t.Fatalf("provider requests = %d", calls.Load())
				}
				for {
					select {
					case info := <-infos:
						if info.Type == "extension_error" && info.Extension == "deciders" {
							return
						}
					case <-ctx.Done():
						t.Fatal("no deciders report")
					}
				}
			}
			if mode == "after-run" || mode == "abort-yield" {
				if err := c.WaitForIdle(ctx); err != nil {
					t.Fatal(err)
				}
				state, err := h.Snapshot(ctx)
				if err != nil {
					t.Fatal(err)
				}
				live := mainContinueBoundaryDocument(t, state, c.ID(), "pi.live")
				if live["run"] != nil || live["runFailure"] != nil {
					t.Fatalf("answered live state = %v", live)
				}
				if mode == "after-run" {
					if len(afterRun) != 1 {
						t.Fatal("afterRun hook was not called once")
					}
					if marker := <-afterRun; marker != nil {
						t.Fatalf("afterRun still sees yield = %v", marker)
					}
				}
			}
		})
	}
}
