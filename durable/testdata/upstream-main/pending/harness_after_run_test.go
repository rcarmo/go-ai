package durable

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Pinned f9a26365 harness-after-run.test.ts:181-219 and :251-276.
// A declaration-only overlay supplies the absent API, never its behaviour.
// This fixture stays outside default discovery until the real lifecycle passes.
type mainAfterRunProbe struct {
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
				id, err := tx.MintID()
				if err != nil {
					return nil, err
				}
				err = tx.AppendEntry(Entry{ID: id, Conversation: r.ConversationID(), Kind: "test.tests", Value: JSON{"label": label}, ByTask: r.TaskID()})
				return taskDone(JSON{"label": label}), err
			})
		}}, Abort: taskAbort})
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
	mainTraceBackends(t, func(t *testing.T, b backend) {
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
		if err != nil || !ok || child.Owner != submissionEntryID(run["task"]) || child.Background {
			t.Fatal("afterRun ownership/background", child, ok, err)
		}
		select {
		case err := <-idle:
			t.Fatal("run became idle with held afterRun task", err)
		default:
		}
		entries, err := c.Entries(bg, EntryCursor{}, 100)
		if err != nil {
			t.Fatal(err)
		}
		var first, answer ID
		for _, entry := range entries {
			if entry.Kind != "message" {
				continue
			}
			var receipt MessageReceipt
			if err := fromObject(entry.Value, &receipt, h.session.limits); err != nil {
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
		entries, err = c.Entries(bg, EntryCursor{}, 100)
		if err != nil || entries[len(entries)-1].Kind != "test.tests" {
			t.Fatal("child result is not the last entry", entries, err)
		}
		generation, ok, err := h.Task(bg, submissionEntryID(run["task"]))
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
func TestMainAfterRunNoHookImmediateEnd(t *testing.T) {
	mainAfterRunEmpty(t, false)
}
func TestMainAfterRunEmptyHookEnd(t *testing.T) {
	mainAfterRunEmpty(t, true)
}
func mainAfterRunEmpty(t *testing.T, hook bool) {
	t.Helper()
	mainTraceBackends(t, func(t *testing.T, b backend) {
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
