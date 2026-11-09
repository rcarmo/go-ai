package durable

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// Behavioural tests for the read-gate layer, which must give a waiting reader the
// same observable behaviour as upstream's `touch(key)` on a suspending line: the
// line is NOT held across a gate, and the reader returns holding a healthy line.
//
// A part stops gating when its publication STARTS, since the writer unregisters
// it there; adoption then runs synchronously behind the Session line. So these
// tests hold a part unpublished by blocking the storage call, which is the window
// a reader is actually meant to wait out.

func gatedSession(t *testing.T) *Session {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	store, err := openSQLiteTestDirectory(dir, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close(bg) })
	return session
}

// holdingQueue admits a part to a writer whose storage call blocks until the
// returned channel closes, so the part stays unpublished and keys every key.
func holdingQueue(t *testing.T, s *Session, keys ...string) chan struct{} {
	t.Helper()
	release := make(chan struct{})
	entered := make(chan struct{})
	previous := s.writer
	t.Cleanup(func() { s.writer = previous })
	writer := newSessionWriter(sessionWriterHost{
		commit: func(_ context.Context, _ []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
			close(entered)
			<-release
			// A stored batch must deliver its receipt, or the writer correctly
			// fails the Session for committing without adoption.
			receive(commitReceipt{index: 0, seq: 1})
			return commitGroupResult{stored: 1, lastSeq: 1}, nil
		},
		publishing: func(_ context.Context, publish func()) <-chan struct{} {
			publish()
			return nil
		},
		failure: s.writerFailure,
		fail:    s.writerFail,
		report:  s.writerReport,
		hop:     func() {},
	})
	s.writer = writer
	writer.admit(sessionWriterSpec{
		index:   1,
		batch:   &Batch{Writes: []Write{{Op: "append-entry"}}},
		keys:    keys,
		ctx:     bg,
		publish: func(*commitReceipt) *writerPublishFailure { return nil },
		discard: func() {},
	})
	<-entered
	return release
}

func TestSessionV150TouchReleasesLineAcrossGate(t *testing.T) {
	s := gatedSession(t)
	key := `doc:["k","conversation",1,null]`
	release := holdingQueue(t, s, key)
	if s.gateFor(key) == nil {
		t.Fatal("keyed gate expected while a part is unpublished")
	}

	waited := make(chan error, 1)
	go func() {
		if err := s.enter(bg); err != nil {
			waited <- err
			return
		}
		waited <- s.touch(bg, key)
	}()

	// The waiter is off the line, so the line must still be acquirable. If touch
	// held the line across a gate wait, this admission would block and fail here.
	select {
	case <-s.line:
	case <-time.After(5 * time.Second):
		t.Fatal("line stayed held across a gate wait")
	}
	// The waiter is still gated while the line is free: the wait is not holding it.
	select {
	case <-waited:
		t.Fatal("touch returned before its gate opened")
	default:
	}
	s.leave()
	close(release)

	if err := <-waited; err != nil {
		t.Fatalf("touch after the gate: %v", err)
	}
	// touch returns holding the line.
	s.leave()
}

func TestSessionV150TouchAllWaitsForNewestPart(t *testing.T) {
	s := gatedSession(t)
	// The ALL_KEYS barrier is the drained() edge: the newest admitted part.
	release := holdingQueue(t, s, "a")
	if s.gateFor(ALL_KEYS) == nil {
		t.Fatal("ALL_KEYS barrier expected while a part is unpublished")
	}
	if s.gateFor("unrelated") != nil {
		t.Error("a key no part writes must not gate")
	}

	errCh := make(chan error, 1)
	go func() {
		if err := s.enter(bg); err != nil {
			errCh <- err
			return
		}
		errCh <- s.touchAll(bg)
	}()
	select {
	case <-s.line:
	case <-time.After(5 * time.Second):
		t.Fatal("ALL_KEYS barrier held the line")
	}
	select {
	case <-errCh:
		t.Fatal("touchAll returned before drained")
	default:
	}
	s.leave()
	close(release)
	if err := <-errCh; err != nil {
		t.Fatalf("touchAll after drained: %v", err)
	}
	s.leave()
}

// The hit is per job and line-owned, so it is only observable to the job that
// took it: a gated job records it, a job that waited at nothing does not.
func TestSessionV150TouchRecordsPerJobHit(t *testing.T) {
	s := gatedSession(t)
	key := `doc:["k","conversation",1,null]`

	if err := s.enter(bg); err != nil {
		t.Fatal(err)
	}
	if s.currentJobHit {
		t.Error("a fresh job must not carry a hit")
	}
	if err := s.touch(bg, key); err != nil {
		t.Fatalf("no part to wait for must not error: %v", err)
	}
	if s.currentJobHit {
		t.Error("a job that waited at nothing must not record a hit")
	}
	s.leave()

	release := holdingQueue(t, s, key)
	if err := s.enter(bg); err != nil {
		t.Fatal(err)
	}
	if s.currentJobHit {
		t.Error("the hit must not leak from a previous job")
	}
	gate := s.gateFor(key)
	s.leave()
	close(release)
	<-gate

	if err := s.enter(bg); err != nil {
		t.Fatal(err)
	}
	if s.currentJobHit {
		t.Error("the hit must not leak into a later job either")
	}
	s.leave()
}

func TestSessionV150TouchReassertsFailure(t *testing.T) {
	s := gatedSession(t)
	key := `doc:["k","conversation",1,null]`
	release := holdingQueue(t, s, key)
	cause := errors.New("backend cause")

	errCh := make(chan error, 1)
	go func() {
		if err := s.enter(bg); err != nil {
			errCh <- err
			return
		}
		errCh <- s.touch(bg, key)
	}()
	// Let the waiter park, then fail the Session while it is off the line.
	time.Sleep(50 * time.Millisecond)
	s.writerFail(cause, 1, nil)
	close(release)

	err := <-errCh
	var failed *SessionFailed
	if !errors.As(err, &failed) || !errors.Is(failed.Cause, cause) {
		t.Fatalf("touch must re-assert the failure that arrived off-line, got %v", err)
	}
}

func TestSessionV150TouchHonoursCancellation(t *testing.T) {
	s := gatedSession(t)
	key := `doc:["k","conversation",1,null]`
	release := holdingQueue(t, s, key)
	defer close(release)

	ctx, cancel := context.WithCancel(bg)
	errCh := make(chan error, 1)
	go func() {
		if err := s.enter(ctx); err != nil {
			errCh <- err
			return
		}
		errCh <- s.touch(ctx, key)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation while gated: %v", err)
	}
}
