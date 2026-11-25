package durable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// harness-after-run.test.ts:984–1043. The worker is conversation-owned;
// the second case cuts off an answered generation during its first hook.
// Kill the real first SQLite writer and lower the limit only once the next
// invocation has actually started under the original limit.
func TestSourceV160LoweredRestartLimitAllowsNormalCompletion(t *testing.T) {
	for _, mode := range []string{"worker", "finish"} {
		t.Run(mode, func(t *testing.T) { testLoweredLimitAfterProcessLossV160(t, mode) })
	}
}

func testLoweredLimitAfterProcessLossV160(t *testing.T, mode string) {
	t.Helper()
	const crashDir = "GOAI_TEST_LOWERED_LIMIT_CRASH"
	const taskKind = "test.worker"
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	type cutoff struct {
		Conversation, Task, Submission, First, Answer ID
	}
	var witness cutoff
	dir := os.Getenv(crashDir)
	crashing := dir != ""
	if !crashing {
		dir = t.TempDir()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceV160LoweredRestartLimitAllowsNormalCompletion$/^"+mode+"$")
		cmd.Env = append(os.Environ(), crashDir+"="+dir)
		output, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || ctx.Err() != nil {
			t.Fatalf("original writer did not reach cutoff: %v\n%s", err, output)
		}
		if status, ok := exit.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
			t.Fatalf("original writer did not exit via SIGKILL: %v\n%s", err, output)
		}
		data, err := os.ReadFile(filepath.Join(dir, "cutoff.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &witness); err != nil || witness.Task == 0 {
			t.Fatal("invalid original cutoff receipt", witness, err)
		}
	}
	store, err := openSQLiteTestDirectory(filepath.Join(dir, "db"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(bg) })
	release := make(chan struct{})
	defer releaseTaskGate(release)
	var providerCalls, asks, tasks atomic.Int32
	hooks := make(chan RunSummary, 1)
	entered := make(chan TaskRecord, 1)
	p := &mainAfterRecoveryProbe{}
	p.ref, p.options = setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		providerCalls.Add(1)
		out := make(chan goai.Event, 1)
		out <- terminal("done")
		close(out)
		return out
	})
	const oldLimit = uint64(5)
	limit := oldLimit
	p.options.Settings = &HarnessSettings{RestartLimit: &limit}
	p.options.OnReportWithInfo = func(_ context.Context, _ error, info ReportInfo) {
		p.mu.Lock()
		p.infos = append(p.infos, info)
		p.mu.Unlock()
	}
	invoke := func(run context.Context, task TaskRecord, r *TaskRuntime) error {
		n := tasks.Add(1)
		if mode == "worker" {
			if n == 1 {
				entered <- task
			}
			select {
			case <-release:
			case <-run.Done():
				return run.Err()
			}
			return r.Commit(run, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		}
		label := generationObject(task.Input.Value)["label"]
		return r.Commit(run, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			if _, err := tx.AppendEntry(r.ConversationID(), EntryDraft{"kind": "test.tests", "data": label}); err != nil {
				return nil, err
			}
			return taskDone(label), nil
		})
	}
	work, err := DefineTask(TaskDefinitionOptions{
		Kind: taskKind, Version: 1,
		Initial: func(any) (JSON, error) { return JSON{"phase": "run"}, nil },
		Phases:  map[string]TaskPhase{"run": invoke}, Abort: taskAbort,
	})
	if err != nil {
		t.Fatal(err)
	}
	exts := &Extension{Name: "worker", Tasks: []*TaskDefinition{work}}
	if mode == "finish" {
		exts.Hooks.AfterRun = func(run context.Context, summary RunSummary, _ *HookAPI) (*AfterRunResult, error) {
			if asks.Add(1) == 1 {
				hooks <- summary
			}
			select {
			case <-release:
			case <-run.Done():
				return nil, run.Err()
			}
			label := "run 1"
			if !crashing {
				label = "run 2"
			}
			return &AfterRunResult{Tasks: []RunEndTask{{Task: work, Input: JSON{"label": label}}}}, nil
		}
	}
	p.options.Registry = NewRegistry()
	if err := p.options.Registry.Install(exts); err != nil {
		t.Fatal(err)
	}
	if crashing {
		if mode == "finish" {
			_, c, answer := mainAfterRecoveryStart(t, p, store)
			summary := watchReceive(t, hooks)
			witness = cutoff{c.ID(), answer.GenerationID, answer.Record.ID, answer.Record.Entry, answer.Record.Answer}
			if asks.Load() != 1 || providerCalls.Load() != 1 || tasks.Load() != 0 || summary.Answer != witness.Answer {
				t.Fatal("wrong original finish cutoff", summary, witness)
			}
		} else {
			h := openHarness(t, store, p.options)
			c := root(t, h, p.ref)
			id, err := c.Commit(ctx, func(tx *Tx) (any, error) {
				return tx.CreateTask(work, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			if first := watchReceive(t, entered); first.ID != id.(ID) || first.Restarts != 0 || tasks.Load() != 1 {
				t.Fatal("wrong original worker cutoff", first)
			}
			witness = cutoff{Conversation: c.ID(), Task: id.(ID)}
		}
		data, err := json.Marshal(witness)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cutoff.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		t.Fatal("SIGKILL did not stop the original writer", syscall.Kill(os.Getpid(), syscall.SIGKILL))
	}
	before, found, err := store.Task(ctx, witness.Task)
	phase := "run"
	if mode == "finish" {
		phase = "finish"
	}
	if err != nil || !found || before.Owner != 0 || before.Conversation != witness.Conversation || before.State.Status != "running" || before.Restarts != 0 || taskCheckpointMap(before.State.Checkpoint)["phase"] != phase {
		t.Fatal("SQLite lacked the held original invocation", before, found, err)
	}
	if mode == "finish" {
		if page, err := store.ScanTasks(ctx, TaskQuery{Conversation: witness.Conversation, Kind: taskKind}, 8, nil); err != nil || page.Next != nil || len(page.Items) != 0 {
			t.Fatal("the first hook had already published children", page, err)
		}
	}
	h, c := mainAfterRecoveryOpen(t, p, store, witness.Conversation)
	pending, found, err := h.Task(ctx, witness.Task)
	if err != nil || !found || pending.State.Status != "pending" || pending.Restarts != 1 || !equalJSONValue(pending.State.Checkpoint, before.State.Checkpoint) {
		t.Fatal("did not recover exactly the held invocation", pending, found, err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if mode == "finish" {
		if got := watchReceive(t, hooks); got != (RunSummary{Reason: "answer", First: witness.First, Answer: witness.Answer, Last: witness.Answer}) {
			t.Fatal("wrong retried finish hook summary", got)
		}
	} else if got := watchReceive(t, entered); got.ID != witness.Task || got.Restarts != 1 {
		t.Fatal("wrong retried worker", got)
	}
	if running, found, err := h.Task(ctx, witness.Task); err != nil || !found || running.State.Status != "running" || running.Restarts != 1 || h.RestartLimit() != oldLimit {
		t.Fatal("retry did not enter at the old limit", running, found, err, h.RestartLimit())
	}
	lowered := uint64(1)
	if err := h.SetSettings(ctx, HarnessSettings{RestartLimit: &lowered}); err != nil || h.RestartLimit() != lowered {
		t.Fatal("could not lower restart limit during retry", h.RestartLimit(), err)
	}
	releaseTaskGate(release)
	final, err := h.WaitForTask(ctx, witness.Task)
	outcome := final.State.Outcome
	if err != nil || final.Owner != before.Owner || final.State.Status != "terminal" || final.Restarts != 0 || outcome == nil || outcome.Status != "completed" || outcome.Result == nil || !outcome.Result.Present {
		t.Fatal("in-flight retry failed to complete normally", final, err)
	}
	if mode == "worker" {
		if outcome.Result.Value != nil || asks.Load() != 0 || providerCalls.Load() != 0 || tasks.Load() != 1 {
			t.Fatal("wrong worker completion or callback counts", outcome.Result, asks.Load(), providerCalls.Load(), tasks.Load())
		}
	} else {
		if err := c.WaitForIdle(ctx); err != nil {
			t.Fatal(err)
		}
		if !equalJSONValue(outcome.Result.Value, JSON{"entryId": float64(witness.Answer)}) || asks.Load() != 1 || providerCalls.Load() != 0 || tasks.Load() != 1 {
			t.Fatal("wrong answered finish or callback counts", outcome.Result, asks.Load(), providerCalls.Load(), tasks.Load())
		}
		status := mainCutoffStatus(t, h, witness.Submission)
		if status.Status != "done" || status.Answer != witness.Answer {
			t.Fatal("normal finish changed the committed answer", status)
		}
		page, err := c.Entries(ctx, EntryQuery{Order: Ascending}, 16, nil)
		if err != nil || page.Next != nil || len(page.Items) < 2 {
			t.Fatal("missing finished transcript", page.Next, err)
		}
		after := page.Items[len(page.Items)-1]
		if after.Kind != "test.tests" || !after.HasData || after.Data != "run 2" || page.Items[len(page.Items)-2].ID != witness.Answer {
			t.Fatal("expected only the retried hook's child after the original answer", after)
		}
		child, found, err := h.Task(ctx, after.ByTask)
		if err != nil || !found || child.Owner != witness.Task || child.State.Status != "terminal" || child.State.Outcome == nil || child.State.Outcome.Status != "completed" || child.State.Outcome.Result == nil || !child.State.Outcome.Result.Present || child.State.Outcome.Result.Value != "run 2" {
			t.Fatal("wrong joined finish child", child, found, err)
		}
		if live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil); err != nil || !found || !equalJSONValue(live.Copy(), JSON{}) {
			t.Fatal("normal lowered-limit finish left live state", live, found, err)
		}
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, info := range p.infos {
		if info.Type == "task_interrupted" {
			t.Fatal("normal completion emitted task_interrupted", info)
		}
	}
}
