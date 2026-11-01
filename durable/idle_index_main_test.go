package durable

import (
	"context"
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
	utf16Backends(t, func(t *testing.T, b backend) {
		mint := func() ID {
			t.Helper()
			id, err := b.store.MintID(bg)
			if err != nil {
				t.Fatal(err)
			}
			return id
		}
		parent, child, docID := mint(), mint(), mint()
		// Validated legacy generation rows deliberately exercise the exact raw
		// checkpoint contract; no phase hosts or invocation maps are fabricated.
		p := Task{ID: parent, Conversation: 1, Kind: "pi.generation", Status: "pending", Checkpoint: JSON{"phase": "queued"}}
		c := Task{ID: child, Conversation: 1, Owner: parent, Kind: "pi.generation", Status: "pending", Checkpoint: JSON{"phase": "queued"}}
		d := Document{ID: docID, Scope: "conversation", Owner: 1, Kind: "pi.live", Version: 1, Value: JSON{"failedRun": true}}
		if _, err := b.store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &p}, {Op: "put-task", Task: &c}, putDocument(d)}}); err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store)
		checkMainIdleIndex(t, h, true) // Held child is live: parent must NOT be held.
		commit := func(f func(*Tx) error) {
			t.Helper()
			if _, err := h.Commit(bg, f); err != nil {
				t.Fatal(err)
			}
		}
		commit(func(tx *Tx) error { c.Status = "done"; return tx.PutTask(c) })
		checkMainIdleIndex(t, h, false)
		h.session.taskBookkeeping(func() {
			if h.scheduler.idleIndex.rebuilds != 1 || len(h.scheduler.idleIndex.generations[1]) != 1 {
				t.Fatal("publication rebuilt forest or retained terminal generation in document fanout")
			}
		})
		commit(func(tx *Tx) error {
			d, err := tx.Document(docID)
			if err != nil {
				return err
			}
			return d.ApplyOperations([]Operation{{"s", []any{"failedRun"}, false}})
		})
		checkMainIdleIndex(t, h, true)
		commit(func(tx *Tx) error {
			d, err := tx.Document(docID)
			if err != nil {
				return err
			}
			return d.ApplyOperations([]Operation{{"s", []any{"failedRun"}, true}})
		})
		checkMainIdleIndex(t, h, false)
	})
}

func TestMainIdleIndexActualHost(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
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
	utf16Backends(t, func(t *testing.T, b backend) {
		h := taskTestHarness(t, b.store)
		h.session.taskBookkeeping(func() { h.scheduler.currentIdleIndex() })
		var before uint64
		h.session.taskBookkeeping(func() { before = h.scheduler.idleIndex.work })
		if _, err := h.Commit(bg, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"text": "entry only"}})
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
				if _, err := h.Commit(bg, func(tx *Tx) error {
					id, err := tx.MintID()
					if err != nil {
						return err
					}
					cid = id
					return tx.CreateConversation(Conversation{ID: id})
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
	utf16Backends(t, func(t *testing.T, b backend) {
		a, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		bid, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		p := Task{ID: a, Conversation: 1, Owner: bid, Kind: "legacy.idle", Status: "pending", Checkpoint: JSON{}}
		q := Task{ID: bid, Conversation: 1, Kind: "legacy.idle", Status: "pending", Checkpoint: JSON{}}
		mainWakeSourceSeed(t, b.store, p, q)
		h := taskTestHarness(t, b.store)
		checkMainIdleIndex(t, h, true)
		if _, err := h.Commit(bg, func(tx *Tx) error {
			p.Owner = 0
			q.Owner = a
			if err := tx.PutTask(p); err != nil {
				return err
			}
			return tx.PutTask(q)
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
	utf16Backends(t, func(t *testing.T, b backend) {
		id, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		cid, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		c := Conversation{ID: cid, Owner: id}
		task := Task{ID: id, Conversation: cid, Kind: "legacy.idle", Status: "pending", Checkpoint: JSON{}}
		if _, err := b.store.Apply(bg, Batch{Writes: []Write{{Op: "create-conversation", Conversation: &c}, {Op: "put-task", Task: &task}}}); err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store)
		h.session.taskBookkeeping(func() {
			if _, usable := h.scheduler.indexedContextBusy(cid); usable {
				t.Fatal("combined legacy cycle indexed as forest")
			}
			if !h.session.contextBusy(cid) || h.session.contextBusy(0) {
				t.Fatal("cycle fallback changed scope semantics")
			}
		})
	})
}

func TestMainIdleIndexAmbiguousDocumentFallback(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
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
		})
		if _, err := h.Commit(bg, func(tx *Tx) error {
			doc, err := tx.Document(second)
			if err != nil {
				return err
			}
			return doc.Retire()
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
