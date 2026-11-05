package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// Done records that admission reached its blocking select, without sleeps.
type mainCommitAdmissionContext struct {
	context.Context
	entered chan struct{}
	once    sync.Once
}

func (c *mainCommitAdmissionContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.entered) })
	return c.Context.Done()
}

func TestMainSessionUnpublishedReadBarriers(t *testing.T) {
	for _, kind := range []string{"entry", "conversation"} {
		t.Run(kind, func(t *testing.T) {
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			s, err := OpenSession(store)
			if err != nil {
				store.Close(context.Background())
				t.Fatal(err)
			}
			held, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer func() {
				unblock()
				if err := s.Close(context.Background()); err != nil {
					t.Error(err)
				}
			}()
			original := store.appendFrame
			store.appendFrame = func(typ byte, ordinal, high uint64, payload []byte) error {
				if typ == 2 {
					select {
					case <-held:
					default:
						close(held)
						<-release
					}
				}
				if original != nil {
					return original(typ, ordinal, high, payload)
				}
				return nil
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			var leaked ID
			first := make(chan error, 1)
			go func() {
				_, err := s.Commit(ctx, func(tx *Tx) error {
					var err error
					leaked, err = tx.MintID()
					if err != nil {
						return err
					}
					if kind == "entry" {
						return tx.AppendEntry(Entry{ID: leaked, Conversation: 1, Kind: "note", Value: JSON{"body": "first"}})
					}
					return tx.CreateConversation(Conversation{ID: leaked})
				})
				first <- err
			}()
			select {
			case <-held:
			case <-ctx.Done():
				t.Fatal("first write did not reach storage")
			}
			admission := &mainCommitAdmissionContext{Context: ctx, entered: make(chan struct{})}
			second := make(chan error, 1)
			if kind == "entry" {
				go func() {
					state, err := s.Snapshot(admission)
					if err == nil && state.Entries[leaked].ID != leaked {
						t.Error("published entry missing")
					}
					second <- err
				}()
			} else {
				go func() {
					_, err := s.Commit(admission, func(tx *Tx) error {
						id, err := tx.MintID()
						if err != nil {
							return err
						}
						return tx.AppendEntry(Entry{ID: id, Conversation: leaked, Kind: "note", Value: JSON{"body": "second"}})
					})
					second <- err
				}()
			}
			select {
			case <-admission.entered:
			case <-ctx.Done():
				t.Fatal("dependent operation never attempted admission")
			}
			select {
			case err := <-second:
				t.Fatalf("dependent operation settled before publication: %v", err)
			default:
			}
			unblock()
			select {
			case err := <-first:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("first write stuck")
			}
			select {
			case err := <-second:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("dependent operation stuck")
			}
			snapshot, err := s.Snapshot(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "conversation" {
				if snapshot.Conversations[leaked].ID != leaked || len(snapshot.Entries) != 1 {
					t.Fatal("dependent conversation write missing")
				}
			}
		})
	}
}

// Actual f9a26365 session-group-commit.test.ts case 414, in the part native can express. The source
// admits three parts while the first call is in flight and refuses one of them; native admits one
// writer on the line, so the parts run in admission order. What is measured here is the fence native
// does implement: the call that fails at settlement latches the Session, later operations report the
// failure, and the parts stored before it stay durable while the failed part never lands.
//
// The reported cause is native's own classification ("uncertain storage outcome"), not the storage
// error the source expects; that difference is recorded separately, and the pre-admission refusal path
// (which native does not latch at all) stays as a pending oracle.
func TestMainSessionFailureFenceKeepsEarlierPartsAfterSettlementError(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	cause := errors.New("connection lost")
	original := store.appendFrame
	stored := 0
	store.appendFrame = func(typ byte, ordinal, high uint64, payload []byte) error {
		if strings.Contains(string(payload), "lost") {
			return cause
		}
		stored++
		if original != nil {
			return original(typ, ordinal, high, payload)
		}
		return nil
	}
	defer func() { store.appendFrame = original }()

	s, err := OpenSession(store)
	if err != nil {
		store.Close(context.Background())
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var a, b ID
	if _, err := s.Commit(ctx, func(tx *Tx) error {
		var err error
		a, err = tx.MintID()
		if err != nil {
			return err
		}
		if err := tx.CreateConversation(Conversation{ID: a}); err != nil {
			return err
		}
		b, err = tx.MintID()
		if err != nil {
			return err
		}
		return tx.CreateConversation(Conversation{ID: b})
	}); err != nil {
		t.Fatal(err)
	}

	// The earlier part that must be kept.
	if _, err := s.Commit(ctx, func(tx *Tx) error {
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		return tx.AppendEntry(Entry{ID: id, Conversation: a, Kind: "note", Value: JSON{"body": "kept"}})
	}); err != nil {
		t.Fatal(err)
	}

	// The part that fails at settlement.
	if _, err := s.Commit(ctx, func(tx *Tx) error {
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		return tx.AppendEntry(Entry{ID: id, Conversation: b, Kind: "note", Value: JSON{"body": "lost"}})
	}); err == nil {
		t.Fatal("settlement error settled without an error")
	}

	// The Session is latched: a later read reports SessionFailed.
	if _, err := s.Snapshot(ctx); err != nil {
		var failed *SessionFailed
		if !errors.As(err, &failed) {
			t.Fatalf("read after settlement error is not SessionFailed: %v", err)
		}
	} else {
		t.Fatal("read after settlement error returned no error")
	}

	end, err := s.Closed(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if end.Reason != "failed" {
		t.Fatalf("session end must report failed: %+v", end)
	}

	if err := s.Close(ctx); err != nil {
		var failed *SessionFailed
		if !errors.As(err, &failed) {
			t.Fatalf("close after failure: %v", err)
		}
	}

	// Earlier parts stayed durable; the failed part never landed.
	reopened := reopenStoreAfterHarnessClose(t, store)
	defer reopened.Close(ctx)
	s2, err := OpenSession(reopened)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close(ctx)
	snap, err := s2.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entries := 0
	for _, e := range snap.Entries {
		entries++
		if e.Kind != "note" || e.Value["body"] != "kept" {
			t.Fatalf("the failed part landed: %v", e)
		}
	}
	if entries != 1 {
		t.Fatalf("only the earlier part should be stored, got %d (frames written: %d)", entries, stored)
	}
}

// Actual f9a26365 session-group-commit.test.ts gates cases 217, 240, 292, 323, and the two deferred-commit
// ordering cases 662 and 680, in the shape native can express. Native admits one writer on the line, so a
// second job cannot run its callback inside the pending window; what native does is block at admission and
// then observe the published work. Measured: the creating job reaches storage while publication is held, the
// second job does not settle inside the window, and after release it sees the published document (217),
// writes to the published conversation (240), includes the published conversation in an unkeyed scan (292),
// a line read issued after an un-awaited commit waits for that commit and then sees its entry (323), and
// publication order follows admission order (662, 680).
//
// Shape difference recorded: the source counts a write of type document.create, native has no create-type
// write at storage level - an initial document write is put-document and a later change delta-document - so
// the measured equivalent for case 217 is one put-document from the creating job and one delta-document on
// the same id from the waiting job. Cases 662 and 680 are approximations: native cannot have a deferred part
// inside one pending group, so the deferred job is a later writer.
//
// Fixture shape notes: values must be reported from inside the callback and settlement awaited separately,
// because a job that reports after its own commit cannot be observed inside the pending window and a subtest
// that ends before settlement cancels the context under a still-running commit. An earlier version timed out
// at 60s for the first reason and failed intermittently for the second; both were fixture artifacts, not
// contract results. A third artifact - an injected-failure branch returning while the hook mutex was held -
// deadlocked a run for 90s and is not credited.
func TestMainSessionGatesJobsWaitForPublication(t *testing.T) {
	def, err := DefineDocument(DefinitionOptions{
		Kind:           "gc.gates.note",
		Version:        1,
		Scope:          "session",
		Initial:        func(JSON) (JSON, error) { return JSON{"n": 0}, nil },
		CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("case-217-document-address-waits", func(t *testing.T) {
		s, _, held, ctx := openGatesSession(t)
		held.holdCommits()

		values := make(chan int64, 4)
		firstSettled := make(chan error, 1)
		secondSettled := make(chan error, 1)

		bump := func(t *testing.T, settled chan error) {
			runGatesJob(t, s, ctx, func(tx *Tx) error {
				h, err := tx.AcquireDocument(def, 0, nil, nil)
				if err != nil {
					return err
				}
				v, err := h.Get()
				if err != nil {
					return err
				}
				n := gateNumber(v["n"]) + 1
				if err := h.Set(JSON{"n": n}); err != nil {
					return err
				}
				values <- n
				return nil
			}, settled)
		}

		bump(t, firstSettled)
		select {
		case n := <-values:
			if n != 1 {
				t.Fatalf("creating job must produce 1, got %d", n)
			}
		case err := <-firstSettled:
			t.Fatalf("creating job settled inside the pending window: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("creating job did not reach storage while held")
		}

		bump(t, secondSettled)
		select {
		case n := <-values:
			t.Fatalf("waiting job ran inside the pending window with n=%d", n)
		case err := <-secondSettled:
			t.Fatalf("waiting job errored inside the pending window: %v", err)
		case <-time.After(400 * time.Millisecond):
		}

		held.releaseHold()
		select {
		case n := <-values:
			if n != 2 {
				t.Fatalf("waiting job must read the published document and produce 2, got %d", n)
			}
		case err := <-secondSettled:
			t.Fatalf("waiting job errored after release: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("waiting job stuck after release")
		}
		for _, ch := range []chan error{firstSettled, secondSettled} {
			if err := settleWithinGate(t, ch, 3); err != nil {
				t.Fatalf("commit did not settle: %v", err)
			}
		}
		if held.putFrames != 1 || held.deltaFrames != 1 {
			t.Fatalf("waiting job must write a delta on the existing document: put=%d delta=%d", held.putFrames, held.deltaFrames)
		}
	})

	t.Run("case-240-write-waits-for-created-conversation", func(t *testing.T) {
		s, _, held, ctx := openGatesSession(t)
		held.holdCommits()

		values := make(chan ID, 4)
		createdSettled := make(chan error, 1)
		appendedSettled := make(chan error, 1)
		var createdID ID

		runGatesJob(t, s, ctx, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.CreateConversation(Conversation{ID: id}); err != nil {
				return err
			}
			createdID = id
			values <- id
			return nil
		}, createdSettled)

		select {
		case id := <-values:
			createdID = id
		case err := <-createdSettled:
			t.Fatalf("creating commit settled inside the pending window: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("creating job did not reach storage while held")
		}

		runGatesJob(t, s, ctx, func(tx *Tx) error {
			entryID, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: entryID, Conversation: createdID, Kind: "note", Value: JSON{"text": "note"}}); err != nil {
				return err
			}
			values <- entryID
			return nil
		}, appendedSettled)

		select {
		case id := <-values:
			t.Fatalf("write job ran inside the pending window (entry %d)", id)
		case err := <-appendedSettled:
			t.Fatalf("write job errored inside the pending window: %v", err)
		case <-time.After(400 * time.Millisecond):
		}

		held.releaseHold()
		select {
		case entryID := <-values:
			if entryID == 0 {
				t.Fatal("no entry written")
			}
		case err := <-appendedSettled:
			t.Fatalf("write job errored after release: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("write job stuck after release")
		}
		for _, ch := range []chan error{createdSettled, appendedSettled} {
			if err := settleWithinGate(t, ch, 3); err != nil {
				t.Fatalf("commit did not settle: %v", err)
			}
		}

		snap, err := s.Snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, e := range snap.Entries {
			if e.Conversation == createdID {
				found = true
			}
		}
		if !found {
			t.Fatalf("appended entry must belong to the published conversation %d, snapshot: %v", createdID, snap.Entries)
		}
	})

	t.Run("case-292-unkeyed-scan-waits", func(t *testing.T) {
		s, _, held, ctx := openGatesSession(t)
		held.holdCommits()

		values := make(chan ID, 4)
		createdSettled := make(chan error, 1)
		scannedSettled := make(chan error, 1)
		var createdID ID

		runGatesJob(t, s, ctx, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.CreateConversation(Conversation{ID: id}); err != nil {
				return err
			}
			createdID = id
			values <- id
			return nil
		}, createdSettled)

		select {
		case id := <-values:
			createdID = id
		case err := <-createdSettled:
			t.Fatalf("creating commit settled inside the pending window: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("creating job did not reach storage while held")
		}

		seen := make(chan []ID, 1)
		go func() {
			_, err := s.Commit(ctx, func(tx *Tx) error {
				page, err := tx.ScanConversations(ConversationQuery{}, 10, "")
				if err != nil {
					return err
				}
				ids := []ID{}
				for _, c := range page.Items {
					ids = append(ids, c.ID)
				}
				seen <- ids
				return nil
			})
			scannedSettled <- err
		}()

		select {
		case ids := <-seen:
			t.Fatalf("unkeyed scan ran inside the pending window: %v", ids)
		case err := <-scannedSettled:
			t.Fatalf("unkeyed scan errored inside the pending window: %v", err)
		case <-time.After(400 * time.Millisecond):
		}

		held.releaseHold()
		select {
		case ids := <-seen:
			found := false
			for _, id := range ids {
				if id == createdID {
					found = true
				}
			}
			if !found {
				t.Fatalf("scan after publication must include the published conversation %d, got %v", createdID, ids)
			}
		case err := <-scannedSettled:
			t.Fatalf("unkeyed scan errored after release: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("unkeyed scan stuck after release")
		}
		for _, ch := range []chan error{createdSettled, scannedSettled} {
			if err := settleWithinGate(t, ch, 3); err != nil {
				t.Fatalf("commit did not settle: %v", err)
			}
		}
	})

	t.Run("case-323-line-read-after-un-awaited-commit", func(t *testing.T) {
		s, _, held, ctx := openGatesSession(t)
		held.holdCommits()

		values := make(chan ID, 4)
		commitSettled := make(chan error, 1)
		var convID ID

		runGatesJob(t, s, ctx, func(tx *Tx) error {
			created, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.CreateConversation(Conversation{ID: created}); err != nil {
				return err
			}
			entryID, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: entryID, Conversation: created, Kind: "note", Value: JSON{"text": "note"}}); err != nil {
				return err
			}
			convID = created
			values <- created
			return nil
		}, commitSettled)

		select {
		case id := <-values:
			convID = id
		case err := <-commitSettled:
			t.Fatalf("un-awaited commit settled inside the pending window: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("un-awaited commit did not reach storage while held")
		}

		read := make(chan Snapshot, 1)
		go func() {
			snap, err := s.Snapshot(ctx)
			if err != nil {
				t.Errorf("line read: %v", err)
			}
			read <- snap
		}()
		select {
		case snap := <-read:
			if len(snap.Entries) != 0 {
				t.Fatalf("line read returned inside the pending window with entries: %v", snap.Entries)
			}
			t.Fatal("line read did not wait for the un-awaited commit's publication")
		case <-time.After(400 * time.Millisecond):
		}

		held.releaseHold()
		select {
		case snap := <-read:
			if len(snap.Entries) != 1 {
				t.Fatalf("line read after publication must see the entry the un-awaited commit created, got %v", snap.Entries)
			}
			// Native's line starts at conversation 1, so assert the created conversation is present
			// rather than that it is the only one.
			if _, ok := snap.Conversations[convID]; !ok {
				t.Fatalf("line read after publication must see the created conversation %d, got %v", convID, snap.Conversations)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("line read stuck after release")
		}
		if err := settleWithinGate(t, commitSettled, 3); err != nil {
			t.Fatalf("un-awaited commit did not settle: %v", err)
		}
	})

	t.Run("case-662-deferred-commit-publishes-after-the-previous", func(t *testing.T) {
		s, _, held, ctx := openGatesSession(t)
		held.holdCommits()

		values := make(chan ID, 4)
		firstSettled := make(chan error, 1)
		secondSettled := make(chan error, 1)

		runGatesJob(t, s, ctx, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"text": "first"}}); err != nil {
				return err
			}
			values <- id
			return nil
		}, firstSettled)

		select {
		case id := <-values:
			if id == 0 {
				t.Fatal("no entry written")
			}
		case err := <-firstSettled:
			t.Fatalf("first commit settled inside the pending window: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("first commit did not reach storage while held")
		}

		runGatesJob(t, s, ctx, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"text": "second"}}); err != nil {
				return err
			}
			values <- id
			return nil
		}, secondSettled)

		select {
		case id := <-values:
			t.Fatalf("deferred job ran inside the pending window (entry %d)", id)
		case err := <-secondSettled:
			t.Fatalf("deferred job errored inside the pending window: %v", err)
		case <-time.After(400 * time.Millisecond):
		}

		held.releaseHold()
		if err := settleWithinGate(t, firstSettled, 3); err != nil {
			t.Fatalf("first commit did not settle: %v", err)
		}
		if err := settleWithinGate(t, secondSettled, 3); err != nil {
			t.Fatalf("deferred commit did not settle: %v", err)
		}
		ordinals := held.landedOrdinals()
		if len(ordinals) != 2 || ordinals[0] > ordinals[1] {
			t.Fatalf("publication order must follow admission order, got %v", ordinals)
		}
	})

	t.Run("case-680-deferred-commit-behind-a-failed-commit-never-publishes", func(t *testing.T) {
		s, _, held, ctx := openGatesSession(t)
		held.holdCommits()
		held.failOn = `"refuse"`

		values := make(chan ID, 4)
		failedSettled := make(chan error, 1)
		laterSettled := make(chan error, 1)

		runGatesJob(t, s, ctx, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "refuse", Value: JSON{"text": "refuse"}}); err != nil {
				return err
			}
			values <- id
			return nil
		}, failedSettled)

		select {
		case id := <-values:
			if id == 0 {
				t.Fatal("no entry staged")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("refused commit did not reach storage while held")
		}

		runGatesJob(t, s, ctx, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{"text": "after"}}); err != nil {
				return err
			}
			return nil
		}, laterSettled)

		held.releaseHold()

		err := settleWithinGate(t, failedSettled, 3)
		if err == nil {
			t.Fatal("refused commit must fail")
		}
		// Native reports its own classification rather than the storage error, so the source's error
		// identity is not preserved (already recorded through the pending refusal fixture).
		if !strings.Contains(err.Error(), "refused") && !strings.Contains(err.Error(), "uncertain storage outcome") {
			t.Fatalf("refused commit must report the refusal or native classification, got %v", err)
		}

		err = settleWithinGate(t, laterSettled, 3)
		if err == nil {
			t.Fatal("job after a refused commit must be refused, not settled")
		}
		if !strings.Contains(err.Error(), "SessionFailed") && !strings.Contains(err.Error(), "session failed") {
			t.Fatalf("job after a refused commit must report SessionFailed, got %v", err)
		}
		if len(held.landedOrdinals()) != 0 {
			t.Fatalf("no part may publish after a refused commit, landed frames: %v", held.landedOrdinals())
		}
	})
}

// Actual f9a26365 session-group-commit.test.ts case 256: each read of the entries of a conversation an
// unpublished commit creates waits for its creation and then finds nothing. Source line reads use
// Session.readOnLine, not raw Storage. The separate v1.5.0 read-line test also retains both
// transaction newest-entry and head-marker surfaces; all five are expressible natively.
func TestMainSessionCase256ReadSurfacesWaitForPendingCreation(t *testing.T) {
	surfaces := []struct {
		name   string
		expect string
		read   func(t *testing.T, s *Session, store *SQLiteStorage, conv ID) (string, bool, error)
	}{
		{
			name:   "tx-scanEntries",
			expect: "empty items",
			read: func(t *testing.T, s *Session, store *SQLiteStorage, conv ID) (string, bool, error) {
				page, err := s.ScanEntries(context.Background(), EntryQuery{Conversation: conv}, 1, "")
				if err != nil {
					return "", false, err
				}
				return "empty items", len(page.Items) == 0, nil
			},
		},
		{
			name:   "line-scanEntries",
			expect: "empty items",
			read: func(t *testing.T, s *Session, store *SQLiteStorage, conv ID) (string, bool, error) {
				var page Page[Entry]
				err := s.ReadOnLine(context.Background(), func(line *ReadLine) error {
					var err error
					page, err = line.ScanEntries(EntryQuery{Conversation: conv}, 1, "")
					return err
				})
				if err != nil {
					return "", false, err
				}
				return "empty items", len(page.Items) == 0, nil
			},
		},
		{
			name:   "line-findLatestHeadMarker",
			expect: "not found",
			read: func(t *testing.T, s *Session, store *SQLiteStorage, conv ID) (string, bool, error) {
				var found bool
				err := s.ReadOnLine(context.Background(), func(line *ReadLine) error {
					var err error
					_, found, err = line.FindLatestHeadMarker(conv, 0)
					return err
				})
				if err != nil {
					return "", false, err
				}
				return "not found", !found, nil
			},
		},
	}

	for _, surface := range surfaces {
		t.Run(surface.name, func(t *testing.T) {
			s, store, held, ctx := openGatesSession(t)
			held.holdCommits()

			created := make(chan ID, 1)
			createdSettled := make(chan error, 1)
			var conv ID

			runGatesJob(t, s, ctx, func(tx *Tx) error {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				if err := tx.CreateConversation(Conversation{ID: id}); err != nil {
					return err
				}
				created <- id
				return nil
			}, createdSettled)

			select {
			case id := <-created:
				conv = id
			case err := <-createdSettled:
				t.Fatalf("creating commit settled inside the pending window: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("creating job did not reach storage while held")
			}

			result := make(chan string, 1)
			errs := make(chan error, 1)
			go func() {
				got, ok, err := surface.read(t, s, store, conv)
				if err != nil {
					errs <- err
					return
				}
				if !ok {
					result <- "unexpected " + got
					return
				}
				result <- got
			}()

			select {
			case got := <-result:
				t.Fatalf("read was not gated by the pending creation (returned %s inside the window)", got)
			case err := <-errs:
				t.Fatalf("read errored inside the pending window: %v", err)
			case <-time.After(400 * time.Millisecond):
			}

			held.releaseHold()
			select {
			case got := <-result:
				if got != surface.expect {
					t.Fatalf("read after publication must return %s, got %s", surface.expect, got)
				}
			case err := <-errs:
				t.Fatalf("read after publication errored: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("read stuck after release")
			}
			if err := settleWithinGate(t, createdSettled, 3); err != nil {
				t.Fatalf("creating commit did not settle: %v", err)
			}
		})
	}
}

// Counts the write types each job produced at storage level: native encodes a first document write
// as put-document and a later change as delta-document, so no create-type write is expected.
type mainGatesHeld struct {
	mu          sync.Mutex
	hold        bool
	entered     chan struct{}
	release     chan struct{}
	putFrames   int
	deltaFrames int
	landed      []uint64
	failOn      string
}

func (h *mainGatesHeld) holdCommits() { h.mu.Lock(); h.hold = true; h.mu.Unlock() }

func (h *mainGatesHeld) releaseHold() {
	h.mu.Lock()
	h.hold = false
	h.mu.Unlock()
	select {
	case <-h.release:
	default:
		close(h.release)
	}
}

func (h *mainGatesHeld) install(appendFrame func(byte, uint64, uint64, []byte) error) func(byte, uint64, uint64, []byte) error {
	return func(typ byte, ordinal, high uint64, payload []byte) error {
		h.mu.Lock()
		if bytes.Contains(payload, []byte(`"put-document"`)) {
			h.putFrames++
		}
		if bytes.Contains(payload, []byte(`"delta-document"`)) {
			h.deltaFrames++
		}
		fail := h.failOn != "" && bytes.Contains(payload, []byte(h.failOn))
		hold := typ == 2 && h.hold
		h.mu.Unlock()

		if hold {
			select {
			case <-h.entered:
			default:
				close(h.entered)
			}
			<-h.release
		}

		if fail {
			return errors.New("refused")
		}

		if appendFrame == nil {
			return nil
		}
		err := appendFrame(typ, ordinal, high, payload)
		if err == nil && typ == 2 {
			h.mu.Lock()
			h.landedLocked(ordinal)
			h.mu.Unlock()
		}
		return err
	}
}

func (h *mainGatesHeld) landedLocked(ordinal uint64) {
	if len(h.landed) < 16 {
		h.landed = append(h.landed, ordinal)
	}
}

func (h *mainGatesHeld) landedOrdinals() []uint64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]uint64, len(h.landed))
	copy(out, h.landed)
	return out
}

func gateNumber(v any) int64 {
	switch t := v.(type) {
	case json.Number:
		n, err := t.Int64()
		if err != nil {
			return -1
		}
		return n
	case int64:
		return t
	case int:
		return int64(t)
	}
	return -1
}

func openGatesSession(t *testing.T) (*Session, *SQLiteStorage, *mainGatesHeld, context.Context) {
	t.Helper()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	held := &mainGatesHeld{entered: make(chan struct{}), release: make(chan struct{})}
	original := store.appendFrame
	store.appendFrame = held.install(original)
	t.Cleanup(func() { store.appendFrame = original })
	s, err := OpenSession(store)
	if err != nil {
		store.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close(context.Background()) })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	// Cleanups run in reverse order. Release before cancellation and Close:
	// otherwise an early assertion failure can leave Close waiting forever
	// for a commit blocked in the append hook.
	t.Cleanup(held.releaseHold)
	return s, store, held, ctx
}

// runGatesJob reports the callback value from inside the pending window and settlement separately, so the
// test can tell "ran but not published" from "settled".
func runGatesJob(t *testing.T, s *Session, ctx context.Context, callback func(*Tx) error, settled chan error) {
	t.Helper()
	go func() {
		_, err := s.Commit(ctx, callback)
		settled <- err
	}()
}

func settleWithinGate(t *testing.T, ch chan error, seconds int) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(time.Duration(seconds) * time.Second):
		return fmt.Errorf("no settlement within %ds", seconds)
	}
}

// A subtest may fail or return before explicitly releasing a held append. Its
// cleanup must release the hook before waiting for Session.Close. The buffered
// completion channel lets the parent verify the commit goroutine returned.
func TestMainGatesSessionCleanupReleasesHeldAppend(t *testing.T) {
	done := make(chan error, 1)
	if !t.Run("return-with-held-append", func(t *testing.T) {
		s, _, held, ctx := openGatesSession(t)
		held.holdCommits()
		go func() {
			_, err := s.Commit(ctx, func(tx *Tx) error {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "note", Value: JSON{}})
			})
			done <- err
		}()
		select {
		case <-held.entered:
		case err := <-done:
			t.Fatalf("commit returned before the held append: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("commit did not reach the held append")
		}
		// Deliberately rely on cleanup, as an early assertion failure would.
	}) {
		t.Fatal("held-append subtest failed")
	}
	select {
	case <-done: // A cancelled or successful commit can both unwind safely.
	case <-time.After(3 * time.Second):
		t.Fatal("held commit did not return after cleanup")
	}
}
