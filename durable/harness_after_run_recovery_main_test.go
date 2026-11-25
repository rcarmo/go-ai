package durable

// Official v1.5.0 f9a26365 harness-after-run.test.ts sources429/757/955/983/1019.
// Native process-loss adapter fails the actual Session, joins its host and
// reopens the backend. No task/checkpoint/restart markers are fabricated.
import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type mainAfterRecoveryProbe struct {
	asks        atomic.Int64
	calls       atomic.Int64
	tasks       atomic.Int64
	hookEntered chan struct{}
	taskEntered chan struct{}
	hooks       map[int64]chan struct{}
	gates       map[int64]chan struct{}
	mu          sync.Mutex
	labels      []string
	infos       []ReportInfo
	options     Options
	ref         ModelRef
}

func mainAfterRecoverySetup(t *testing.T, hooks, gates map[int64]chan struct{}, limit *uint64) *mainAfterRecoveryProbe {
	t.Helper()
	p := &mainAfterRecoveryProbe{hookEntered: make(chan struct{}, 16), taskEntered: make(chan struct{}, 16), hooks: hooks, gates: gates}
	def, err := DefineTask(TaskDefinitionOptions{Kind: "task.after-recovery", Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil },
		Phases: map[string]TaskPhase{"run": func(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
			n := p.tasks.Add(1)
			label := generationObject(task.Input.Value)["label"].(string)
			p.mu.Lock()
			p.labels = append(p.labels, label)
			p.mu.Unlock()
			p.taskEntered <- struct{}{}
			if gate := p.gates[n]; gate != nil {
				select {
				case <-gate:
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
				return taskDone(nil), err
			})
		}}, Abort: taskAbort})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Install(&Extension{Name: "after-recovery", Tasks: []*TaskDefinition{def}, Hooks: GenerationHooks{AfterRun: func(ctx context.Context, _ RunSummary, _ *HookAPI) (*AfterRunResult, error) {
		n := p.asks.Add(1)
		p.hookEntered <- struct{}{}
		if gate := p.hooks[n]; gate != nil {
			select {
			case <-gate:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return &AfterRunResult{Tasks: []RunEndTask{{Task: def, Input: JSON{"label": fmt.Sprintf("run %d", n)}}}}, nil
	}}}); err != nil {
		t.Fatal(err)
	}
	p.ref, p.options = setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		p.calls.Add(1)
		ch := make(chan goai.Event, 1)
		ch <- terminal("done")
		close(ch)
		return ch
	})
	p.options.Registry = registry
	p.options.Settings = &HarnessSettings{RestartLimit: limit}
	p.options.OnReportWithInfo = func(_ context.Context, _ error, info ReportInfo) {
		p.mu.Lock()
		p.infos = append(p.infos, info)
		p.mu.Unlock()
	}
	return p
}
func mainAfterRecoveryCrash(t *testing.T, h *Harness, store Storage) Storage {
	t.Helper()
	return (&mainRecoveryHarness{t: t, h: h, store: store}).crash()
}
func mainAfterRecoveryOpen(t *testing.T, p *mainAfterRecoveryProbe, store Storage, id ID) (*Harness, *ConversationHandle) {
	t.Helper()
	h := openHarness(t, store, p.options)
	c, err := h.Conversation(bg, id)
	if err != nil {
		t.Fatal(err)
	}
	return h, c
}
func mainAfterRecoveryIdle(t *testing.T, c *ConversationHandle) {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 3*time.Second)
	defer cancel()
	if err := c.WaitForIdle(ctx); err != nil {
		t.Error("answered run did not become idle", err)
	}
}

type mainAfterRecoveryRun struct {
	Record       SubmissionRecord
	GenerationID ID
}

func mainAfterRecoveryStart(t *testing.T, p *mainAfterRecoveryProbe, store Storage) (*Harness, *ConversationHandle, mainAfterRecoveryRun) {
	t.Helper()
	h := openHarness(t, store, p.options)
	c := root(t, h, p.ref)
	sub, err := c.Submit(bg, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	record := waitSubmission(t, sub).Record
	live := mainAfterRunLive(t, h, c.ID())
	generation := submissionEntryID(generationObject(live["run"])["taskId"])
	if generation == 0 {
		t.Fatal("answered run missing its public generation", live)
	}
	return h, c, mainAfterRecoveryRun{Record: record, GenerationID: generation}
}
func mainAfterRecoveryOutcome(t *testing.T, h *Harness, id ID) {
	t.Helper()
	task, ok, err := h.Task(bg, id)
	if err != nil || !ok || task.State.Status != "terminal" || task.State.Outcome == nil || task.State.Outcome.Status != "completed" {
		t.Error("answered generation outcome", task, ok, err)
	}
}
func mainAfterRecoveryReports(t *testing.T, p *mainAfterRecoveryProbe, kind, reason string, want bool) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	found := false
	for _, info := range p.infos {
		if info.Type == kind && (reason == "" || info.Reason == reason) {
			found = true
		}
	}
	if found != want {
		t.Errorf("report %s/%s present=%v want=%v; infos=%+v", kind, reason, found, want, p.infos)
	}
}

func TestMainAfterRunCrashFinishReasksDrainDoesNot(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		p := mainAfterRecoverySetup(t, map[int64]chan struct{}{1: make(chan struct{})}, map[int64]chan struct{}{1: make(chan struct{})}, nil)
		h, c, settled := mainAfterRecoveryStart(t, p, b.store)
		awaitTaskSignal(t, p.hookEntered)
		store := mainAfterRecoveryCrash(t, h, b.store)
		h, c = mainAfterRecoveryOpen(t, p, store, c.ID())
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, p.hookEntered)
		awaitTaskSignal(t, p.taskEntered)
		if p.asks.Load() != 2 {
			t.Error("finish must reask", p.asks.Load())
		}
		store = mainAfterRecoveryCrash(t, h, store)
		h, c = mainAfterRecoveryOpen(t, p, store, c.ID())
		mainAfterRecoveryIdle(t, c)
		if p.asks.Load() != 2 || p.calls.Load() != 1 {
			t.Error("drain reasked or redispatched", p.asks.Load(), p.calls.Load())
		}
		p.mu.Lock()
		labels := append([]string(nil), p.labels...)
		p.mu.Unlock()
		if !reflect.DeepEqual(labels, []string{"run 2", "run 2"}) {
			t.Error("same owned task replay", labels)
		}
		entries, err := c.Entries(bg, EntryQuery{Order: Ascending}, 100, nil)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, entry := range entries.Items {
			if entry.Kind == "test.tests" {
				count++
			}
		}
		if count != 1 {
			t.Error("task completion entries", count)
		}
		inspect, err := h.InspectTasks(bg)
		if err != nil || len(inspect.Tasks) != 0 {
			t.Error("unfinished tasks", inspect, err)
		}
		mainAfterRecoveryOutcome(t, h, settled.GenerationID)
	})
}

// harness-after-run.test.ts:758-783 uses restartLimit=1 with a queued input.
func TestMainAfterRunAnsweredFinishRestartLimit(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) { testAfterRunFinishAfterProcessLoss(t, "restart-limit") })
}

func TestMainAfterRunChildRestartLimitCompletesGeneration(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		limit := uint64(1)
		p := mainAfterRecoverySetup(t, nil, map[int64]chan struct{}{1: make(chan struct{})}, &limit)
		h, c, settled := mainAfterRecoveryStart(t, p, b.store)
		awaitTaskSignal(t, p.taskEntered)
		store := mainAfterRecoveryCrash(t, h, b.store)
		h, c = mainAfterRecoveryOpen(t, p, store, c.ID())
		mainAfterRecoveryIdle(t, c)
		mainAfterRecoveryOutcome(t, h, settled.GenerationID)
		if live := mainAfterRunLive(t, h, c.ID()); live["runFailure"] != nil || live["run"] != nil {
			t.Error("child cutoff failed or retained answered run", live)
		}
		p.mu.Lock()
		labels := append([]string(nil), p.labels...)
		p.mu.Unlock()
		if !reflect.DeepEqual(labels, []string{"run 1"}) {
			t.Error("cutoff child reran", labels)
		}
		if err := h.Close(bg); err != nil {
			t.Error(err)
		}
		mainAfterRecoveryReports(t, p, "task_faulted", "restart_limit", true)
	})
}

func TestMainAfterRunLoweredLimitNormalFinishNoInterrupted(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		release := make(chan struct{})
		p := mainAfterRecoverySetup(t, map[int64]chan struct{}{1: make(chan struct{}), 2: release}, nil, nil)
		h, c, _ := mainAfterRecoveryStart(t, p, b.store)
		awaitTaskSignal(t, p.hookEntered)
		store := mainAfterRecoveryCrash(t, h, b.store)
		h, c = mainAfterRecoveryOpen(t, p, store, c.ID())
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, p.hookEntered)
		limit := uint64(1)
		if err := h.SetSettings(bg, HarnessSettings{RestartLimit: &limit}); err != nil {
			t.Fatal(err)
		}
		close(release)
		mainAfterRecoveryIdle(t, c)
		p.mu.Lock()
		labels := append([]string(nil), p.labels...)
		p.mu.Unlock()
		if !reflect.DeepEqual(labels, []string{"run 2"}) {
			t.Error("normal finish after lower limit", labels)
		}
		if err := h.Close(bg); err != nil {
			t.Error(err)
		}
		mainAfterRecoveryReports(t, p, "task_interrupted", "", false)
	})
}

func TestMainAfterRunLoweredLimitNormalWorkerNoInterrupted(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		p := mainAfterRecoverySetup(t, nil, nil, nil)
		started := make(chan struct{}, 2)
		var runs atomic.Int64
		def := taskDefinition(t, "task.lowered-limit-worker", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			n := runs.Add(1)
			started <- struct{}{}
			if n == 1 {
				<-ctx.Done()
				return ctx.Err()
			}
			limit := uint64(1)
			if err := r.harness.SetSettings(ctx, HarnessSettings{RestartLimit: &limit}); err != nil {
				return err
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if err := p.options.Registry.Install(&Extension{Name: "worker", Tasks: []*TaskDefinition{def}}); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, p.options)
		c := root(t, h, p.ref)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, started)
		store := mainAfterRecoveryCrash(t, h, b.store)
		h, _ = mainAfterRecoveryOpen(t, p, store, c.ID())
		task := waitPublicTask(t, h, id)
		if task.State.Outcome == nil || task.State.Outcome.Status != "completed" {
			t.Error("worker outcome", task)
		}
		if err := h.Close(bg); err != nil {
			t.Error(err)
		}
		mainAfterRecoveryReports(t, p, "task_interrupted", "", false)
	})
}

// Source: harness-hook-tasks.test.ts, afterTools is asked again before its
// decision tasks exist, and not once their IDs and the wait have been stored.
func TestSourceV160GenerationAfterToolsTaskCrash(t *testing.T) {
	const witnessEnv = "GOAI_TEST_AFTER_TOOLS_CRASH_DIR"
	dir := os.Getenv(witnessEnv)
	crashing := dir != ""
	ctx, cancel := context.WithTimeout(bg, 14*time.Second)
	defer cancel()
	var addresses struct{ Conversation, Submission ID }
	if !crashing {
		dir = t.TempDir()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceV160GenerationAfterToolsTaskCrash$")
		cmd.Env = append(os.Environ(), witnessEnv+"="+dir)
		output, err := cmd.CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if ctx.Err() != nil || !ok || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
			t.Fatalf("child did not crash at the hook: %v\n%s", err, output)
		}
		data, err := os.ReadFile(filepath.Join(dir, "addresses.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &addresses); err != nil {
			t.Fatal(err)
		}
	}
	store, err := OpenSQLite(filepath.Join(dir, "store.sqlite"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	var requests, asked atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		request := requests.Add(1)
		out := make(chan goai.Event, 1)
		if crashing && request == 1 {
			out <- toolAnswer("c1", "noop", JSON{})
		} else {
			out <- terminal("done")
		}
		close(out)
		return out
	})
	const kind = "test.aftertools-recovery"
	token := testSignalAnswer(t)
	key, err := token.Key("go")
	if err != nil {
		t.Fatal(err)
	}
	atHook := make(chan struct{}, 1)
	parked := make(chan TaskRecord, 1)
	child, err := DefineTask(TaskDefinitionOptions{Kind: kind, Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "park"}, nil }, Phases: map[string]TaskPhase{
		"park": func(run context.Context, record TaskRecord, r *TaskRuntime) error {
			err := r.Commit(run, func(*Tx, TaskRecord) (*TaskState, error) {
				return &TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "done"}, Signals: []SignalKey{key}}, nil
			})
			if err == nil {
				parked <- record
			}
			return err
		},
		"done": func(run context.Context, _ TaskRecord, r *TaskRuntime) error {
			return r.Commit(run, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		},
	}, Abort: func(run context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(run, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	options.Registry = NewRegistry()
	if err := options.Registry.Install(&Extension{Name: "deciders", Tools: []ToolRegistration{wrapRegistration("noop")}, Tasks: []*TaskDefinition{child}, Hooks: GenerationHooks{
		AfterToolsTasks: func(run context.Context, _ ID, _ []ID, _ *HookAPI) (*HookTasksResult, error) {
			asked.Add(1)
			if crashing {
				atHook <- struct{}{}
				<-run.Done()
				return nil, run.Err()
			}
			return &HookTasksResult{Tasks: []RunEndTask{{Task: child}}}, nil
		},
	}}); err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if crashing {
		c := root(t, h, ref)
		sub, err := c.Submit(ctx, Input{Type: "input", Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-atHook:
		case <-ctx.Done():
			t.Fatal("first host never asked", ctx.Err())
		}
		state, err := h.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, record := range state.Tasks {
			if record.Kind == kind {
				t.Fatal("decision task stored before the hook returned")
			}
		}
		if asked.Load() != 1 || requests.Load() != 1 {
			t.Fatalf("first host asked=%d requests=%d", asked.Load(), requests.Load())
		}
		addresses.Conversation, addresses.Submission = c.ID(), sub.ID()
		data, err := json.Marshal(addresses)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "addresses.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		return
	}
	var decision TaskRecord
	select {
	case decision = <-parked:
	case <-ctx.Done():
		t.Fatal("reopened host did not ask and park", ctx.Err())
	}
	if asked.Load() != 1 || requests.Load() != 0 {
		t.Fatalf("reopened host asked=%d requests=%d", asked.Load(), requests.Load())
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened := reopenStoreAfterHarnessClose(t, store)
	h = openHarness(t, reopened, options)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	park, found, err := h.Task(ctx, decision.ID)
	if err != nil || !found || park.State.Status != "sleeping" || park.Owner != decision.Owner {
		t.Fatalf("decision on reopen: %+v, found=%v, %v", park, found, err)
	}
	parent, found, err := h.Task(ctx, decision.Owner)
	if err != nil || !found || parent.Version != 3 || parent.State.Status != "waiting" || taskCheckpointMap(parent.State.Checkpoint)["phase"] != "afterTools" || !reflect.DeepEqual(parent.State.On, []ID{decision.ID}) {
		t.Fatalf("generation wait on reopen: %+v, found=%v, %v", parent, found, err)
	}
	if delivered, err := h.Signal(ctx, decision.ID, token, "go", JSON{"text": "done"}); err != nil || delivered != SignalDelivered {
		t.Fatalf("signal %v, %v", delivered, err)
	}
	sub, err := h.Submission(ctx, addresses.Submission)
	if err != nil {
		t.Fatal(err)
	}
	if result, err := sub.Wait(ctx); err != nil || result.Record.Status != "done" || result.Record.Conversation != addresses.Conversation || asked.Load() != 1 || requests.Load() != 1 {
		t.Fatalf("final submission=%+v, %v; asked=%d requests=%d", result.Record, err, asked.Load(), requests.Load())
	}
}
