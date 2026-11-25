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

func testAfterRunTwoSQLiteCutoffsV160(t *testing.T) {
	const dirEnv, stageEnv = "GOAI_TEST_AFTER_RUN_REPLAY_DIR", "GOAI_TEST_AFTER_RUN_REPLAY_STAGE"
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	dir, stage := os.Getenv(dirEnv), os.Getenv(stageEnv)
	if stage == "" {
		dir = t.TempDir()
		for _, next := range []string{"finish", "drain"} {
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMainAfterRunCrashFinishReasksDrainDoesNot$/^sqlite$")
			cmd.Env = append(os.Environ(), dirEnv+"="+dir, stageEnv+"="+next)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || ctx.Err() != nil {
				t.Fatalf("%s writer missed the cutoff: %v\n%s", next, err, output)
			}
			if status, ok := exit.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
				t.Fatalf("%s writer did not exit via SIGKILL: %v\n%s", next, err, output)
			}
		}
	}
	store, err := openSQLiteTestDirectory(filepath.Join(dir, "db"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(bg) })
	// The hand-off file contains identities observed in actual commits. It
	// never supplies a task, outcome, restart count, or document to SQLite.
	type cutoff struct {
		Conversation, Generation, Answer, Submission, Child ID
	}
	var witness cutoff
	path := filepath.Join(dir, "cutoff.json")
	kill := func(witness cutoff) {
		t.Helper()
		data, err := json.Marshal(witness)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		t.Fatal("SIGKILL did not stop the original writer", syscall.Kill(os.Getpid(), syscall.SIGKILL))
	}
	if stage == "finish" {
		p := mainAfterRecoverySetup(t, map[int64]chan struct{}{1: make(chan struct{})}, nil, nil)
		_, c, answer := mainAfterRecoveryStart(t, p, store)
		watchReceive(t, p.hookEntered)
		if p.asks.Load() != 1 || p.calls.Load() != 1 || p.tasks.Load() != 0 || answer.Record.Status != "done" || answer.Record.Answer == 0 {
			t.Fatal("first writer did not hold the first hook after the answer", p.asks.Load(), p.calls.Load(), p.tasks.Load(), answer.Record)
		}
		kill(cutoff{Conversation: c.ID(), Generation: answer.GenerationID, Answer: answer.Record.Answer, Submission: answer.Record.ID})
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &witness); err != nil || witness.Generation == 0 || witness.Answer == 0 {
		t.Fatal("invalid cutoff identity receipt", witness, err)
	}
	parent, found, err := store.Task(ctx, witness.Generation)
	if err != nil || !found || parent.Conversation != witness.Conversation || parent.Restarts != 0 {
		t.Fatal("missing original generation at raw SQLite cutoff", parent, found, err)
	}
	checkpoint := taskCheckpointMap(parent.State.Checkpoint)
	beforeChildren, err := store.ScanTasks(ctx, TaskQuery{Conversation: witness.Conversation, Kind: "task.after-recovery"}, 8, nil)
	if err != nil || beforeChildren.Next != nil {
		t.Fatal("invalid child scan at raw SQLite cutoff", beforeChildren, err)
	}
	release := make(chan struct{})
	defer releaseTaskGate(release)
	p := mainAfterRecoverySetup(t, nil, map[int64]chan struct{}{1: release}, nil)
	// The first host's one ask was already witnessed. The retried hook
	// labels its child run 2 without seeding the local attempt counter.
	p.labelOffset = 1
	if stage == "drain" {
		if witness.Child != 0 || parent.State.Status != "running" || checkpoint["phase"] != "finish" || submissionEntryID(generationObject(checkpoint["run"])["answer"]) != witness.Answer || len(beforeChildren.Items) != 0 {
			t.Fatal("first cutoff did not retain finish with no children", parent, beforeChildren, witness)
		}
	} else {
		if witness.Child == 0 || parent.State.Status != "waiting" || checkpoint["phase"] != "drain" || submissionEntryID(checkpoint["answer"]) != witness.Answer || !slices.Equal(parent.State.On, []ID{witness.Child}) || len(beforeChildren.Items) != 1 {
			t.Fatal("second cutoff did not retain drain and one owned child", parent, beforeChildren, witness)
		}
		child := beforeChildren.Items[0]
		if child.ID != witness.Child || child.Owner != witness.Generation || child.State.Status != "running" || child.Restarts != 0 || generationObject(child.Input.Value)["label"] != "run 2" {
			t.Fatal("wrong original child at drain cutoff", child)
		}
	}
	h, c := mainAfterRecoveryOpen(t, p, store, witness.Conversation)
	if stage == "drain" {
		if task, ok, err := h.Task(ctx, witness.Generation); err != nil || !ok || task.State.Status != "pending" || task.Restarts != 1 || !equalJSONValue(task.State.Checkpoint, parent.State.Checkpoint) {
			t.Fatal("finish did not recover its one interrupted attempt", task, ok, err)
		}
	} else {
		if task, ok, err := h.Task(ctx, witness.Generation); err != nil || !ok || task.State.Status != "waiting" || task.Restarts != 0 || !slices.Equal(task.State.On, []ID{witness.Child}) {
			t.Fatal("waiting generation was interrupted during child recovery", task, ok, err)
		}
		if child, ok, err := h.Task(ctx, witness.Child); err != nil || !ok || child.Owner != witness.Generation || child.State.Status != "pending" || child.Restarts != 1 {
			t.Fatal("drain did not recover the same child and its one restart", child, ok, err)
		}
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	idle := make(chan error, 1)
	go func() { idle <- c.WaitForIdle(ctx) }()
	select {
	case <-p.taskEntered:
	case err := <-idle:
		t.Fatal("run became idle without the required child attempt", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	p.mu.Lock()
	labels := slices.Clone(p.labels)
	p.mu.Unlock()
	if !slices.Equal(labels, []string{"run 2"}) || p.tasks.Load() != 1 || p.calls.Load() != 0 {
		t.Fatal("wrong held child callback or provider replay", labels, p.tasks.Load(), p.calls.Load())
	}
	if stage == "drain" {
		if p.asks.Load() != 1 {
			t.Fatal("finish did not reask exactly once before creating its child", p.asks.Load())
		}
		live := mainAfterRunLive(t, h, c.ID())
		run := generationObject(live["run"])
		children, _ := generationObject(run["afterRun"])["tasks"].([]any)
		if len(children) != 1 || submissionEntryID(run["taskId"]) != witness.Generation || submissionEntryID(children[0]) == 0 {
			t.Fatal("retried finish did not publish exactly one child", live)
		}
		witness.Child = submissionEntryID(children[0])
		kill(witness)
	}
	if task, ok, err := h.Task(ctx, witness.Child); err != nil || !ok || task.Owner != witness.Generation || task.State.Status != "running" || task.Restarts != 1 {
		t.Fatal("did not replay the same owned child", task, ok, err)
	}
	releaseTaskGate(release)
	if err := watchReceive(t, idle); err != nil {
		t.Fatal(err)
	}
	page, err := c.Entries(ctx, EntryQuery{Order: Ascending}, 16, nil)
	if err != nil || page.Next != nil {
		t.Fatal("missing complete short transcript", page.Next, err)
	}
	var writes []Entry
	for _, entry := range page.Items {
		if entry.Kind == "test.tests" {
			writes = append(writes, entry)
		}
	}
	if len(writes) != 1 || writes[0].ByTask != witness.Child || writes[0].Value["label"] != "run 2" {
		t.Fatal("expected exactly one committed write from the same replayed child", writes)
	}
	children, err := store.ScanTasks(ctx, TaskQuery{Conversation: witness.Conversation, Kind: "task.after-recovery"}, 8, nil)
	if err != nil || children.Next != nil || len(children.Items) != 1 {
		t.Fatal("drain did not retain exactly one child incarnation", children, err)
	}
	child := children.Items[0]
	if child.ID != witness.Child || child.Owner != witness.Generation || child.State.Status != "terminal" || child.Restarts != 0 || child.State.Outcome == nil || child.State.Outcome.Status != "completed" {
		t.Fatal("same replayed child did not complete normally", child)
	}
	if view, err := h.Inspect(ctx); err != nil || len(view.Tasks) != 0 {
		t.Fatal("inspect retained work after child replay", view, err)
	}
	if parent, ok, err := h.Task(ctx, witness.Generation); err != nil || !ok || parent.State.Status != "terminal" || parent.State.Outcome == nil || parent.State.Outcome.Status != "completed" || parent.State.Outcome.Result == nil || !equalJSONValue(parent.State.Outcome.Result.Value, JSON{"entryId": float64(witness.Answer)}) {
		t.Fatal("replay changed the original answered generation", parent, ok, err)
	}
	if report := mainCutoffStatus(t, h, witness.Submission); report.Status != "done" || report.Answer != witness.Answer {
		t.Fatal("replay changed the already committed answer", report)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if p.asks.Load() != 0 || p.tasks.Load() != 1 || p.calls.Load() != 0 {
		t.Fatal("drain reasked the hook or redispatched extra work", p.asks.Load(), p.tasks.Load(), p.calls.Load())
	}
}
