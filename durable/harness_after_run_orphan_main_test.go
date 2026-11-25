package durable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"
)

// harness-after-run.test.ts:786–816 uses a storage gate to simulate loss
// while a real after-run child is in flight. The native SQLite writer dies
// inside that child's held invocation, so reopen needs no record mutation.
func TestMainAfterRunMissingDefinitionStopOrphans(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	store, witness := afterRunChildSQLiteCutoff(t, ctx, "^TestMainAfterRunMissingDefinitionStopOrphans$", nil)

	p := mainAfterRecoverySetup(t, nil, nil, nil)
	p.options.Registry = NewRegistry()
	h, c := mainAfterRecoveryOpen(t, p, store, witness.Conversation)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil)
	if err != nil || !found {
		t.Fatal("missing LiveDoc on reopen", live, found, err)
	}
	contents := generationObject(live.Copy())
	run := generationObject(contents["run"])
	tasks, _ := generationObject(run["afterRun"])["tasks"].([]any)
	if len(tasks) != 1 || submissionEntryID(tasks[0]) != witness.Child || submissionEntryID(run["taskId"]) != witness.Generation || contents["runFailure"] != nil {
		t.Fatal("lost after-run ownership on reopen", contents)
	}
	// The persisted child is pending after public recovery; the inspection
	// derives blocked/missing_task from the currently installed definitions.
	view, err := h.Inspect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var blocked []TaskInspection
	for _, item := range view.Tasks {
		if item.Kind == "blocked" {
			blocked = append(blocked, item)
		}
	}
	if len(blocked) != 1 || blocked[0].Record.ID != witness.Child || blocked[0].Record.Owner != witness.Generation || blocked[0].Reason != "missing_task" || blocked[0].Record.State.Status != "pending" || blocked[0].Record.Restarts != 1 {
		t.Fatal("unavailable child not inspected as the sole blocked task", blocked)
	}
	idle := awaitPendingContextIdleWait(t, ctx, h, c)
	if err := c.Abort(ctx); err != nil {
		t.Fatal(err)
	}
	if err := watchReceive(t, idle); err != nil {
		t.Fatal("Stop did not release the registered public idle wait", err)
	}
	live, found, err = c.SnapshotDefinition(ctx, LiveDoc(), nil)
	if err != nil || !found || !equalJSONValue(live.Copy(), JSON{}) {
		t.Fatal("answered Stop retained live run or failure", live, found, err)
	}
	child, found, err := h.Task(ctx, witness.Child)
	if err != nil || !found || child.Owner != witness.Generation || child.State.Status != "terminal" || child.State.Outcome == nil || child.State.Outcome.Status != "orphaned" || child.State.Outcome.Reason != "missing_task" {
		t.Fatal("Stop did not orphan the original unavailable child", child, found, err)
	}
	// Stop aborts the generation's outstanding work, not the already
	// committed answer. No after-run task body executes on the new host.
	parent, found, err := h.Task(ctx, witness.Generation)
	if err != nil || !found || parent.State.Status != "terminal" || parent.State.Outcome == nil || parent.State.Outcome.Status != "aborted" {
		t.Fatal("wrong answered Stop generation outcome", parent, found, err)
	}
	status := mainCutoffStatus(t, h, witness.Submission)
	if status.Status != "done" || status.Answer != witness.Answer {
		t.Fatal("Stop changed the committed answer", status, witness)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if p.asks.Load() != 0 || p.calls.Load() != 0 || p.tasks.Load() != 0 {
		t.Fatal("reopened host redispatched the hook, provider, or child", p.asks.Load(), p.calls.Load(), p.tasks.Load())
	}
}

type afterRunChildCutoff struct {
	Conversation, Generation, Child, Submission, Answer ID
}

// Verify the same actual SQLite cutoff independently of how the next host
// handles the child (definition missing or its restart budget exhausted).
func afterRunChildSQLiteCutoff(t *testing.T, ctx context.Context, testRun string, limit *uint64) (*SQLiteStorage, afterRunChildCutoff) {
	t.Helper()
	const childDir = "GOAI_TEST_AFTER_RUN_CHILD_CRASH"
	if dir := os.Getenv(childDir); dir != "" {
		store, err := openSQLiteTestDirectory(filepath.Join(dir, "db"), SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		p := mainAfterRecoverySetup(t, nil, map[int64]chan struct{}{1: make(chan struct{})}, limit)
		h, c, answered := mainAfterRecoveryStart(t, p, store)
		watchReceive(t, p.taskEntered)
		live := mainAfterRunLive(t, h, c.ID())
		run := generationObject(live["run"])
		tasks, _ := generationObject(run["afterRun"])["tasks"].([]any)
		if len(tasks) != 1 || submissionEntryID(run["taskId"]) != answered.GenerationID || answered.Record.Status != "done" || answered.Record.Answer == 0 {
			t.Fatal("original answered generation did not own one live after-run child", live, answered)
		}
		p.mu.Lock()
		labels := slices.Clone(p.labels)
		p.mu.Unlock()
		if p.asks.Load() != 1 || p.calls.Load() != 1 || p.tasks.Load() != 1 || !slices.Equal(labels, []string{"run 1"}) {
			t.Fatal("wrong original hook, provider, or child count", p.asks.Load(), p.calls.Load(), p.tasks.Load(), labels)
		}
		witness := afterRunChildCutoff{c.ID(), answered.GenerationID, submissionEntryID(tasks[0]), answered.Record.ID, answered.Record.Answer}
		data, err := json.Marshal(witness)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cutoff.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		t.Fatal("SIGKILL did not stop the original writer", syscall.Kill(os.Getpid(), syscall.SIGKILL))
	}

	dir := t.TempDir()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run="+testRun)
	cmd.Env = append(os.Environ(), childDir+"="+dir)
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
	var witness afterRunChildCutoff
	if err := json.Unmarshal(data, &witness); err != nil || witness.Child == 0 {
		t.Fatal("invalid original cutoff receipt", witness, err)
	}
	store, err := openSQLiteTestDirectory(filepath.Join(dir, "db"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(bg) })
	child, found, err := store.Task(ctx, witness.Child)
	if err != nil || !found || child.Owner != witness.Generation || child.Conversation != witness.Conversation || child.Kind != "task.after-recovery" || child.State.Status != "running" || child.Restarts != 0 {
		t.Fatal("SQLite lacked the held child after process loss", child, found, err)
	}
	parent, found, err := store.Task(ctx, witness.Generation)
	checkpoint := taskCheckpointMap(parent.State.Checkpoint)
	if err != nil || !found || parent.State.Status != "waiting" || parent.Restarts != 0 || !slices.Equal(parent.State.On, []ID{witness.Child}) || checkpoint["phase"] != "drain" || submissionEntryID(checkpoint["answer"]) != witness.Answer {
		t.Fatal("SQLite lacked the original generation waiting for its child and holding its answer", parent, found, err)
	}
	return store, witness
}
