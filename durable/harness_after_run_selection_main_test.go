package durable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// harness-after-run.test.ts:883-901. Upstream simulates loss by blocking
// the old store. Here a separate SQLite writer dies inside the first hook.
func TestMainAfterRunHooksDeselectedBeforeRecoveryFinish(t *testing.T) {
	for _, mode := range []string{"deselected", "retained"} {
		t.Run(mode, func(t *testing.T) { testAfterRunHookSelectionAfterProcessLoss(t, mode) })
	}
}

func testAfterRunHookSelectionAfterProcessLoss(t *testing.T, mode string) {
	t.Helper()
	const crashDir = "GOAI_TEST_AFTER_RUN_SELECTION_CRASH"
	ctx, cancel := context.WithTimeout(bg, 15*time.Second)
	defer cancel()
	type cutoff struct {
		Conversation, Generation, Answer ID
		Asks                             int64
	}
	if dir := os.Getenv(crashDir); dir != "" {
		store, err := openSQLiteTestDirectory(filepath.Join(dir, "db"), SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		p := mainAfterRecoverySetup(t, map[int64]chan struct{}{1: make(chan struct{})}, nil, nil)
		_, c, answered := mainAfterRecoveryStart(t, p, store)
		watchReceive(t, p.hookEntered)
		if p.asks.Load() != 1 || p.tasks.Load() != 0 || p.calls.Load() != 1 || answered.Record.Status != "done" || answered.Record.Answer == 0 {
			t.Fatal("expected one held hook after committed answer and before child task", p.asks.Load(), p.tasks.Load(), p.calls.Load(), answered.Record)
		}
		witness := cutoff{Conversation: c.ID(), Generation: readOnlyHarnessRunID(t, c), Answer: answered.Record.Answer, Asks: p.asks.Load()}
		data, err := json.Marshal(witness)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cutoff.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
			t.Fatal(err)
		}
		return
	}

	dir := t.TempDir()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestMainAfterRunHooksDeselectedBeforeRecoveryFinish$/^"+mode+"$")
	cmd.Env = append(os.Environ(), crashDir+"="+dir)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || ctx.Err() != nil {
		t.Fatalf("first writer did not reach cutoff: %v\n%s", err, output)
	}
	if status, ok := exit.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("first writer exit was not SIGKILL: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(dir, "cutoff.json"))
	if err != nil {
		t.Fatal(err)
	}
	var witness cutoff
	if err := json.Unmarshal(data, &witness); err != nil || witness.Asks != 1 {
		t.Fatal("first writer witness", witness, err)
	}
	store, err := openSQLiteTestDirectory(filepath.Join(dir, "db"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(bg) })
	before, found, err := store.Task(ctx, witness.Generation)
	cp := taskCheckpointMap(before.State.Checkpoint)
	if err != nil || !found || before.State.Status != "running" || before.Restarts != 0 || cp["phase"] != "finish" || submissionEntryID(generationObject(cp["run"])["answer"]) != witness.Answer {
		t.Fatal("SQLite lacked held finish intent after process loss", before, found, err)
	}
	if children, err := store.ScanTasks(ctx, TaskQuery{Conversation: witness.Conversation, Kind: "task.after-recovery"}, 10, nil); err != nil || len(children.Items) != 0 || children.Next != nil {
		t.Fatal("first hook had already published a child", children, err)
	}
	p := mainAfterRecoverySetup(t, nil, nil, nil)
	if mode == "deselected" {
		p.options.Registry.Uninstall(&Extension{Name: "after-recovery"})
	}
	h, c := mainAfterRecoveryOpen(t, p, store, witness.Conversation)
	pending, found, err := h.Task(ctx, witness.Generation)
	if err != nil || !found || pending.State.Status != "pending" || pending.Restarts != 1 || !equalJSONValue(pending.State.Checkpoint, before.State.Checkpoint) {
		t.Fatal("SQLite did not recover exactly the held finish intent", pending, found, err)
	}
	if err := c.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	wantAsks, wantTasks := int64(1), int64(0)
	if mode == "retained" {
		// With the same cutoff and the hook still installed, recovery re-asks
		// and starts its child. This proves that finish actually resumed.
		wantAsks, wantTasks = 2, 1
	}
	if asks := witness.Asks + p.asks.Load(); asks != wantAsks {
		t.Fatalf("after-run hook asks after %s reopen = %d, want %d", mode, asks, wantAsks)
	}
	if tasks := p.tasks.Load(); tasks != wantTasks {
		t.Fatalf("after-run child executions after %s reopen = %d, want %d", mode, tasks, wantTasks)
	}
	live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil)
	if err != nil || !found || !equalJSONValue(live.Copy(), JSON{}) {
		t.Fatal("recovered finish left live state", live, found, err)
	}
	after, found, err := h.Task(ctx, witness.Generation)
	outcome := after.State.Outcome
	if err != nil || !found || after.State.Status != "terminal" || outcome == nil || outcome.Status != "completed" || outcome.Result == nil || !outcome.Result.Present || !equalJSONValue(outcome.Result.Value, JSON{"entryId": float64(witness.Answer)}) || p.calls.Load() != 0 {
		t.Fatal("recovered finish did not keep its original answer", after, found, err, p.calls.Load())
	}
}

// harness-after-run.test.ts:904-921. A fork at the committed answer must
// not copy the held after-run state or the queued input from its parent.
func TestMainAfterRunForkStartsIdleWithoutParentQueue(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		ctx, cancel := context.WithTimeout(bg, 8*time.Second)
		defer cancel()
		gate := make(chan struct{})
		p := mainAfterRecoverySetup(t, nil, map[int64]chan struct{}{1: gate}, nil)
		h, c, settled := mainAfterRecoveryStart(t, p, b.store)
		cleanupTaskGates(t, gate)
		watchReceive(t, p.taskEntered)
		later, err := c.Submit(ctx, Input{Type: "input", Content: "later"})
		if err != nil {
			t.Fatal(err)
		}
		read := func(c *ConversationHandle, definition *DocumentDefinition) JSON {
			t.Helper()
			value, found, err := c.SnapshotDefinition(ctx, definition, nil)
			if err != nil || !found {
				t.Fatal("fork fixture document", c.ID(), value, found, err)
			}
			return generationObject(value.Copy())
		}
		parentLive := read(c, LiveDoc())
		run := generationObject(parentLive["run"])
		generationID := submissionEntryID(run["taskId"])
		children, _ := generationObject(run["afterRun"])["tasks"].([]any)
		if generationID == 0 || len(children) != 1 {
			t.Fatal("expected the parent's one held after-run child", parentLive)
		}
		childID := submissionEntryID(children[0])
		assertParentWork := func() {
			t.Helper()
			if child, found, err := h.Task(ctx, childID); err != nil || !found || child.Owner != generationID || child.Conversation != c.ID() || child.State.Status != "running" {
				t.Fatal("parent lost its held after-run child", child, found, err)
			}
			if status, err := later.Status(ctx); err != nil || status.Status != "queued" || status.Entry != 0 {
				t.Fatal("parent input is not queued behind after-run", status, err)
			}
		}
		assertParentWork()
		parentInbox := read(c, InboxDoc())
		items, _ := parentInbox["items"].([]any)
		if len(items) != 1 || submissionEntryID(generationObject(items[0])["id"]) != later.ID() {
			t.Fatal("parent inbox lacks exactly the later input", parentInbox)
		}

		fork, err := c.Fork(ctx, settled.Record.Answer)
		if err != nil {
			t.Fatal(err)
		}
		forkLive := read(fork, LiveDoc())
		if _, present := forkLive["run"]; present {
			t.Fatal("fork copied active parent run", forkLive)
		}
		if inbox := read(fork, InboxDoc()); inbox["items"] != nil && !equalJSONValue(inbox["items"], []any{}) {
			t.Fatal("fork copied parent inbox", inbox)
		}
		if record, found, err := b.store.Conversation(ctx, fork.ID()); err != nil || !found || record.Owner != 0 || record.OwnerConversation != 0 || record.Parent != c.ID() || record.ParentAt != settled.Record.Answer {
			t.Fatal("expected ownerless fork at the committed answer", record, found, err)
		}
		if err := fork.WaitForIdle(ctx); err != nil {
			t.Fatal("fork did not start idle", err)
		}
		// A separate public wait for the still-held parent must remain pending.
		idleParent := awaitPendingContextIdleWait(t, ctx, h, c)
		assertParentWork()
		if !equalJSONValue(read(c, LiveDoc()), parentLive) || !equalJSONValue(read(c, InboxDoc()), parentInbox) || p.tasks.Load() != 1 || p.calls.Load() != 1 {
			t.Fatal("fork changed the parent run or queued input", p.tasks.Load(), p.calls.Load())
		}
		releaseTaskGate(gate)
		if err := watchReceive(t, idleParent); err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
