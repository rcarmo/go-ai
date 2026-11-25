package durable

import (
	"context"
	"slices"
	"testing"
	"time"
)

// harness-after-run.test.ts:354–370. The answer is committed, but Reset must
// queue until the actual owned after-run task has published its write.
func TestSourceV160ResetQueuesBehindAfterRunTask(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	gate := make(chan struct{})
	defer releaseTaskGate(gate)
	p := mainAfterRecoverySetup(t, nil, map[int64]chan struct{}{1: gate}, nil)
	h, c, answered := mainAfterRecoveryStart(t, p, store)
	watchReceive(t, p.taskEntered)
	live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil)
	if err != nil || !found {
		t.Fatal("hold lacks the live generation", live, found, err)
	}
	run := generationObject(generationObject(live.Copy())["run"])
	children, _ := generationObject(run["afterRun"])["tasks"].([]any)
	if len(children) != 1 || submissionEntryID(run["taskId"]) != answered.GenerationID {
		t.Fatal("hold lacks its one after-run child", run)
	}
	childID := submissionEntryID(children[0])
	child, found, err := h.Task(ctx, childID)
	if err != nil || !found || child.Owner != answered.GenerationID || child.State.Status != "running" {
		t.Fatal("after-run child was not held while running", child, found, err)
	}
	read := func(c *ConversationHandle) []Entry {
		t.Helper()
		page, err := c.Entries(ctx, EntryQuery{Order: Ascending}, 16, nil)
		if err != nil || page.Next != nil {
			t.Fatal("expected the entire short transcript", page.Next, err)
		}
		return page.Items
	}
	prefix := read(c)
	if len(prefix) == 0 || prefix[len(prefix)-1].ID != answered.Record.Answer || answered.Record.Status != "done" {
		t.Fatal("missing already committed answer before reset", answered, prefix)
	}
	if err := c.Reset(ctx, ""); err != nil {
		t.Fatal(err)
	}
	held := read(c)
	for _, entry := range held {
		if entry.Kind == "pi.reset" {
			t.Fatal("reset became an entry while the after-run child was held", entry)
		}
	}
	if !slices.EqualFunc(held, prefix, sameAfterRunResetEntry) {
		t.Fatal("reset changed the committed transcript before the child wrote", held)
	}
	inbox, found, err := c.SnapshotDefinition(ctx, InboxDoc(), nil)
	if err != nil || !found {
		t.Fatal("missing inbox after reset admission", inbox, found, err)
	}
	items, _ := generationObject(inbox.Copy())["items"].([]any)
	if len(items) != 1 {
		t.Fatal("reset was not the sole queued item", items)
	}
	item := generationObject(items[0])
	draft := generationObject(item["entry"])
	resetID := submissionEntryID(item["id"])
	if resetID == 0 || item["mode"] != "write" || draft["kind"] != "pi.reset" || draft["head"] != "self" {
		t.Fatal("wrong queued reset", item)
	}
	reset, err := h.Submission(ctx, resetID)
	if err != nil || reset == nil {
		t.Fatal("missing queued reset handle", reset, err)
	}
	if status, err := reset.Status(ctx); err != nil || status.Status != "queued" || status.Entry != 0 {
		t.Fatal("reset was not queued and unplaced", status, err)
	}

	releaseTaskGate(gate)
	if err := c.WaitForIdle(ctx); err != nil {
		t.Fatal(err)
	}
	finished := read(c)
	if len(finished) != len(prefix)+2 || !slices.EqualFunc(finished[:len(prefix)], prefix, sameAfterRunResetEntry) {
		t.Fatal("expected exactly two entries following the original transcript", finished)
	}
	write, marker := finished[len(prefix)], finished[len(prefix)+1]
	if write.Kind != "test.tests" || write.ByTask != childID || marker.Kind != "pi.reset" || write.Seq >= marker.Seq {
		t.Fatal("expected exact committed child-write → reset suffix", write, marker)
	}
	if status, err := reset.Status(ctx); err != nil || status.Status != "done" || status.Entry != marker.ID || marker.Head != marker.ID || len(marker.Model) != 0 {
		t.Fatal("wrong placed reset receipt", status, marker, err)
	}
	child, found, err = h.Task(ctx, childID)
	if err != nil || !found || child.Owner != answered.GenerationID || child.State.Status != "terminal" || child.State.Outcome == nil || child.State.Outcome.Status != "completed" {
		t.Fatal("wrong joined after-run child", child, found, err)
	}
	if p.asks.Load() != 1 || p.calls.Load() != 1 || p.tasks.Load() != 1 {
		t.Fatal("reset duplicated hook, provider, or child execution", p.asks.Load(), p.calls.Load(), p.tasks.Load())
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, c := mainAfterRecoveryOpen(t, p, reopenStoreAfterHarnessClose(t, store), c.ID())
	if !slices.EqualFunc(read(c), finished, sameAfterRunResetEntry) {
		t.Fatal("SQLite reopen changed the ordered transcript")
	}
	if err := reopened.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func sameAfterRunResetEntry(a, b Entry) bool {
	return a.ID == b.ID && a.Kind == b.Kind && a.ByTask == b.ByTask && a.Seq == b.Seq && a.Head == b.Head
}
