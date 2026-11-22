package durable

import (
	"context"
	"slices"
	"testing"
)

// Compare indexed values with the original predicate, with separate explicit
// expectations for held placeholders and physical terminal hosts.
func checkMainIdleIndex(t *testing.T, h *Harness, want bool) {
	t.Helper()
	h.session.taskBookkeeping(func() {
		x := h.scheduler.currentIdleIndex()
		for _, cid := range []ID{0, 1} {
			got, usable := h.scheduler.indexedContextBusy(cid)
			scan := false
			for _, task := range h.session.readImage.Tasks {
				scan = scan || h.scheduler.idleTaskInScope(h.session.readImage, task, cid)
			}
			indexedTargets, targetsOK := h.scheduler.indexedIdleTargets(cid)
			if !targetsOK || !slices.Equal(indexedTargets, h.scheduler.idleTargets(h.session.readImage, cid)) {
				t.Fatalf("scope%d indexed targets=%v usable=%v", cid, indexedTargets, targetsOK)
			}
			if !usable || got != want || scan != want {
				t.Fatalf("scope%d indexed=%v usable=%v scan=%v want=%v", cid, got, usable, scan, want)
			}
		}
		if x.invalid {
			t.Fatal("invalid index")
		}
	})
}

func TestMainIdleIndexHeldQueue(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		mint := func() ID {
			t.Helper()
			id, err := b.store.MintID(bg)
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
		parent, child, docID, childInput := mint(), mint(), mint(), mint()
		// Keep legacy generation checkpoints so the failedRun fanout is tested.
		p := Task{ID: parent, Conversation: 1, Kind: "pi.generation", Status: "pending", Checkpoint: JSON{"phase": "queued"}}
		c := Task{ID: child, Conversation: 1, Owner: parent, Kind: "pi.generation", Status: "pending", Checkpoint: JSON{"phase": "queued", "submission": childInput}}
		sub := Submission{ID: childInput, Conversation: 1, Type: "follow-up", Status: "pending", Value: JSON{}}
		d := Document{ID: docID, Scope: "conversation", Owner: 1, Kind: "pi.live", Version: 1, Value: JSON{"failedRun": true}}
		apply(t, b.store, Write{Op: "put-task", Task: &p}, Write{Op: "put-task", Task: &c}, Write{Op: "put-submission", Submission: &sub}, putDocument(d))
		h := taskTestHarness(t, b.store)
		checkMainIdleIndex(t, h, true) // Held child is live: parent must NOT be held.
		commit := func(f func(*Tx) error) {
			t.Helper()
			if _, err := h.Commit(bg, func(tx *Tx) (any, error) { return nil, f(tx) }); err != nil {
				t.Fatal(err)
			}
		}
		commit(func(tx *Tx) error {
			c.Status, c.Checkpoint = "aborted", JSON{"phase": "terminal", "submission": childInput}
			c.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{Hold: &BuiltinTaskHold{
				Stage: "final", Action: "generation-abort", FinalStatus: "aborted", Conversation: c.Conversation, Owner: c.Owner,
				Submission: childInput, Outcome: TaskOutcome{Status: "aborted"},
			}}}
			return tx.stage(Write{Op: "put-task", Task: &c})
		})
		checkMainIdleIndex(t, h, false)
		h.session.taskBookkeeping(func() {
			if h.scheduler.idleIndex.rebuilds != 1 || len(h.scheduler.idleIndex.generations[1]) != 1 {
				t.Fatal("publication rebuilt forest or retained terminal generation in document fanout")
			}
		})
		commit(func(tx *Tx) error {
			d, err := tx.documentRecord(docID)
			if err != nil {
				return err
			}
			return d.ApplyOperations([]Operation{{"s", []any{"failedRun"}, false}})
		})
		checkMainIdleIndex(t, h, true)
		commit(func(tx *Tx) error {
			d, err := tx.documentRecord(docID)
			if err != nil {
				return err
			}
			return d.ApplyOperations([]Operation{{"s", []any{"failedRun"}, true}})
		})
		checkMainIdleIndex(t, h, false)
	})
}

func TestMainIdleIndexActualHost(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		committed, release := make(chan struct{}), make(chan struct{})
		def := taskDefinition(t, "idle.index.host", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if err := r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil }); err != nil {
				return err
			}
			close(committed)
			<-release
			return nil
		})
		h := taskTestHarness(t, b.store, def)
		cleanupTaskGates(t, release)
		h.session.taskBookkeeping(func() { h.scheduler.currentIdleIndex() })
		createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, committed)
		checkMainIdleIndex(t, h, true)
		releaseTaskGate(release)
		if err := h.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		checkMainIdleIndex(t, h, false)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		if h.scheduler.idleIndex != nil {
			t.Fatal("Close retained idle index")
		}
	})
}

func TestMainIdleIndexPublicationWitness(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		h := taskTestHarness(t, b.store)
		h.session.taskBookkeeping(func() { h.scheduler.currentIdleIndex() })
		var before uint64
		h.session.taskBookkeeping(func() { before = h.scheduler.idleIndex.work })
		if _, err := h.Commit(bg, func(tx *Tx) (any, error) {
			id, err := tx.mintID()
			if err != nil {
				return nil, err
			}
			return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"text": "entry only"}})
		}); err != nil {
			t.Fatal(err)
		}
		h.session.taskBookkeeping(func() {
			if x := h.scheduler.idleIndex; x.work != before || x.rebuilds != 1 || x.image.Seq != h.session.readImage.Seq {
				t.Fatal("entry-only publication visited nodes or lost witness")
			}
		})
		// A private, receipted publication without the production write list must
		// rebuild from the published image, never use a raw future core image.
		id, err := h.session.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		h.session.taskBookkeeping(func() {
			entry := Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"text": "private receipt"}}
			receipt, image, err := h.session.settleBatch(bg, Batch{Writes: []Write{{Op: "append-entry", Entry: &entry}}}, h.session.readImage.Seq)
			if err != nil {
				t.Fatal(err)
			}
			if err := h.session.adoptReceipt(receipt, &image); err != nil {
				t.Fatal(err)
			}
			if h.scheduler.currentIdleIndex().rebuilds != 2 {
				t.Fatal("private adoption reused stale index")
			}
		})
	})
}

func BenchmarkMainContextBusyHistory(b *testing.B) {
	for _, busy := range []bool{false, true} {
		name := "idle"
		if busy {
			name = "busy"
		}
		b.Run(name, func(b *testing.B) {
			h, _, _ := mainWakeIndexFixture(b, 4096)
			cid := ID(1)
			if !busy {
				if _, err := h.Commit(bg, func(tx *Tx) (any, error) {
					id, err := tx.mintID()
					if err != nil {
						return nil, err
					}
					cid = id
					return nil, tx.createConversationRecord(Conversation{ID: id})
				}); err != nil {
					b.Fatal(err)
				}
			}
			h.session.taskBookkeeping(func() {
				if got := h.session.contextBusy(cid); got != busy {
					b.Fatal(got)
				}
				b.ReportAllocs()
				for b.Loop() {
					if got := h.session.contextBusy(cid); got != busy {
						b.Fatal(got)
					}
				}
			})
		})
	}
}

func TestMainIdleIndexLegacyReparent(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		a, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		bid, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		p := fixturePendingTask(a, 1, "legacy.idle")
		p.Owner = bid
		q := fixturePendingTask(bid, 1, "legacy.idle")
		seedRootConversation(t, b.store)
		commitIdleFixtures(t, b.store, StorageWrite{Type: "task", Value: p}, StorageWrite{Type: "task", Value: q})
		h := taskTestHarness(t, b.store)
		checkMainIdleIndex(t, h, true)
		if _, err := h.Commit(bg, func(tx *Tx) (any, error) {
			p.Owner = 0
			q.Owner = a
			if err := tx.setTask(p); err != nil {
				return nil, err
			}
			return nil, tx.setTask(q)
		}); err != nil {
			t.Fatal(err)
		}
		checkMainIdleIndex(t, h, true)
		h.session.taskBookkeeping(func() {
			x := h.scheduler.idleIndex
			if x.rebuilds != 1 || x.nodes[idleScope{conversation: true}].live != 2 {
				t.Fatal("reparent double counted or rebuilt")
			}
		})
	})
}

func TestMainIdleIndexLegacyCycleFallback(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		id, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		cid, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		c := Conversation{ID: cid, Owner: id}
		task := fixturePendingTask(id, cid, "legacy.idle")
		commitIdleFixtures(t, b.store, StorageWrite{Type: "conversation", Value: c}, StorageWrite{Type: "task", Value: task})
		h := taskTestHarness(t, b.store)
		h.session.taskBookkeeping(func() {
			if _, usable := h.scheduler.indexedContextBusy(cid); usable {
				t.Fatal("combined legacy cycle indexed as forest")
			}
			if _, usable := h.scheduler.indexedIdleTargets(cid); usable {
				t.Fatal("cycle target index used")
			}
			if got := h.scheduler.idleTargetsForView(h.session.readImage, cid, true); !slices.Equal(got, []ID{id}) {
				t.Fatal("cycle target fallback", got)
			}
			if !h.session.contextBusy(cid) || h.session.contextBusy(0) {
				t.Fatal("cycle fallback changed scope semantics")
			}
		})
	})
}

func TestMainIdleIndexAmbiguousDocumentFallback(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		mint := func() ID {
			t.Helper()
			id, err := b.store.MintID(bg)
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
		tid, first, second := mint(), mint(), mint()
		task := Task{ID: tid, Conversation: 1, Kind: "pi.generation", Status: "pending", Checkpoint: JSON{"phase": "queued"}}
		d := Document{ID: first, Scope: "conversation", Owner: 1, Kind: "pi.live", Version: 1, Value: JSON{"failedRun": true}}
		e := d
		e.ID = second
		e.Key = "other"
		e.Value = JSON{"failedRun": false}
		if _, err := b.store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &task}, putDocument(d), putDocument(e)}}); err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store)
		h.session.taskBookkeeping(func() {
			if _, usable := h.scheduler.indexedContextBusy(1); usable {
				t.Fatal("conflicting doc flags cached")
			}
			if _, usable := h.scheduler.indexedIdleTargets(1); usable {
				t.Fatal("conflicting document target index used")
			}
		})
		if _, err := h.Commit(bg, func(tx *Tx) (any, error) {
			doc, err := tx.documentRecord(second)
			if err != nil {
				return nil, err
			}
			return nil, doc.Retire()
		}); err != nil {
			t.Fatal(err)
		}
		checkMainIdleIndex(t, h, false)
	})
}

func BenchmarkMainIdleIndexBuild(b *testing.B) {
	h, _, _ := mainWakeIndexFixture(b, 4096)
	h.session.taskBookkeeping(func() {
		b.ReportAllocs()
		for b.Loop() {
			h.scheduler.rebuildIdleIndex()
		}
	})
}

func fixturePendingTask(id, conversation ID, kind string) TaskRecord {
	return TaskRecord{ID: id, Conversation: conversation, Kind: kind, Version: 1, Input: &TaskValue{Present: true}, State: TaskState{Status: "pending", Checkpoint: JSON{}}}
}

func commitIdleFixtures(t *testing.T, store Storage, writes ...StorageWrite) {
	t.Helper()
	result, err := store.Commit(bg, [][]StorageWrite{writes}, nil)
	if err != nil || result.Stored != 1 || result.Refused != nil {
		t.Fatalf("idle fixture refused: %+v, err %v", result, err)
	}
}
