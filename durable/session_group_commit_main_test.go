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

// Actual f9a26365 session-group-commit.test.ts gates cases 217, 240 and 292, in the shape native can
// express. Native admits one writer on the line, so a second job cannot run its callback inside the
// pending window; what native does is wait at admission and then see the published creation. Measured:
// the job waits, it reads the document that the pending commit created instead of creating one again,
// a write to the conversation the pending commit created targets it only after publication, and an unkeyed
// scan includes the published conversation only after publication.
//
// Shape note: native has no `document.create` write type at storage level - create and change are
// `put-document` and `delta-document` - so the source's "creations length 1" assertion has no native
// analogue. The equivalent measured here is that the waiting job produced a delta on the same document
// id, not a second creation.
// Actual f9a26365 session-group-commit.test.ts gates cases 217, 240 and 292, in the shape native can
// express. Native admits one writer on the line, so a second job cannot run its callback inside the
// pending window; what native does is block at admission and then observe the published work. Measured:
// the creating job reaches storage while publication is held, the second job does not settle inside the
// window, and after release it sees the published document (case 217), writes to the published
// conversation (case 240) or includes the published conversation in an unkeyed scan (case 292).
//
// Shape difference recorded: the source counts a write of type document.create, native has no create-type
// write at storage level - an initial document write is put-document and a later change delta-document -
// so the measured equivalent for case 217 is one put-document from the creating job and one
// delta-document on the same id from the waiting job.
//
// Fixture shape note: values must be reported from inside the callback and settlement must be awaited
// separately. A job that reports after its own commit cannot be observed inside the pending window, and a
// subtest that ends before settlement cancels the context under a still-running commit. An earlier
// version of this control timed out at 60s for that reason; that was a fixture artifact, not a contract
// result.
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

	open := func(t *testing.T) (*Session, *mainGatesHeld, context.Context) {
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
		return s, held, ctx
	}

	// run starts one job. The callback reports its value from inside the pending window, and settlement
	// is reported separately so the test can tell "ran but not published" from "settled".
	run := func(t *testing.T, s *Session, ctx context.Context, callback func(*Tx) error, settled chan error) {
		t.Helper()
		go func() {
			_, err := s.Commit(ctx, callback)
			settled <- err
		}()
	}

	settleWithin := func(t *testing.T, ch chan error, seconds int) error {
		t.Helper()
		select {
		case err := <-ch:
			return err
		case <-time.After(time.Duration(seconds) * time.Second):
			return fmt.Errorf("no settlement within %ds", seconds)
		}
	}

	t.Run("case-217-document-address-waits", func(t *testing.T) {
		s, held, ctx := open(t)
		held.holdCommits()

		values := make(chan int64, 4)
		firstSettled := make(chan error, 1)
		secondSettled := make(chan error, 1)

		run(t, s, ctx, func(tx *Tx) error {
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
		}, firstSettled)

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

		run(t, s, ctx, func(tx *Tx) error {
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
		}, secondSettled)

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
			if err := settleWithin(t, ch, 3); err != nil {
				t.Fatalf("commit did not settle: %v", err)
			}
		}
		if held.putFrames != 1 || held.deltaFrames != 1 {
			t.Fatalf("waiting job must write a delta on the existing document: put=%d delta=%d", held.putFrames, held.deltaFrames)
		}
	})

	t.Run("case-240-write-waits-for-created-conversation", func(t *testing.T) {
		s, held, ctx := open(t)
		held.holdCommits()

		values := make(chan ID, 4)
		createdSettled := make(chan error, 1)
		appendedSettled := make(chan error, 1)

		var createdID ID
		run(t, s, ctx, func(tx *Tx) error {
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

		run(t, s, ctx, func(tx *Tx) error {
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
			_ = entryID
		case err := <-appendedSettled:
			t.Fatalf("write job errored after release: %v", err)
		case <-time.After(3 * time.Second):
			t.Fatal("write job stuck after release")
		}
		for _, ch := range []chan error{createdSettled, appendedSettled} {
			if err := settleWithin(t, ch, 3); err != nil {
				t.Fatalf("commit did not settle: %v", err)
			}
		}

		// The appended entry must belong to the conversation the pending commit created.
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
		s, held, ctx := open(t)
		held.holdCommits()

		values := make(chan ID, 4)
		createdSettled := make(chan error, 1)
		scannedSettled := make(chan error, 1)
		var createdID ID

		run(t, s, ctx, func(tx *Tx) error {
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
			if err := settleWithin(t, ch, 3); err != nil {
				t.Fatalf("commit did not settle: %v", err)
			}
		}
	})
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
		if typ == 2 && h.hold {
			h.mu.Unlock()
			select {
			case <-h.entered:
			default:
				close(h.entered)
			}
			<-h.release
		} else {
			h.mu.Unlock()
		}
		if appendFrame != nil {
			return appendFrame(typ, ordinal, high, payload)
		}
		return nil
	}
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
