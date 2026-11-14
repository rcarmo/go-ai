package durable

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func sourceDocumentObserverSession(t *testing.T, conversation bool) (*Session, *SQLiteStorage, *DocumentDefinition, ID) {
	t.Helper()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	session, err := OpenSession(store)
	if err != nil {
		_ = store.Close(context.Background())
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close(context.Background()) })
	options := DefinitionOptions{
		Kind: "observe.p06.source", Version: 1, Scope: "session",
		Initial:        func(JSON) (JSON, error) { return JSON{"count": 0}, nil },
		CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
	}
	owner := ID(0)
	if conversation {
		options.Scope, options.History, options.Fork = "conversation", "latest", "initial"
		owner = 1
	}
	def, err := DefineDocument(options)
	if err != nil {
		t.Fatal(err)
	}
	return session, store, def, owner
}

func sourceDocumentSet(s *Session, def *DocumentDefinition, owner ID, n int, deferred bool) error {
	_, err := s.commitPersistence(context.Background(), func(tx *Tx) error {
		doc, err := tx.AcquireDocument(def, owner, nil, nil)
		if err != nil {
			return err
		}
		return doc.ApplyOperations([]Operation{{"s", []any{"count"}, n}})
	}, true, 0, deferred, math.Inf(1))
	return err
}

// Source77/87/105, adapted through a private standalone synchronous guard.
// Detached Go values and lack of JS thenables remain explicit API differences.
func TestMainSynchronousDocumentObserverSourceAdoption(t *testing.T) {
	t.Run("source77-absent", func(t *testing.T) {
		s, _, def, owner := sourceDocumentObserverSession(t, false)
		calls := 0
		o, baseline, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
			advance: func(context.Context, rawWatchFrame) error { calls++; return nil },
			end:     func(WatchEnd) error { calls++; return nil },
		})
		if err != nil || ok || o != nil || baseline != nil {
			t.Fatalf("absent: handle=%v exists=%v err=%v", o, ok, err)
		}
		if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
			t.Fatal(err)
		}
		if calls != 0 || len(s.documentObservers) != 0 || len(s.localCommitListeners) != 0 {
			t.Fatalf("absent observer attached: calls=%d observers=%d listeners=%d", calls, len(s.documentObservers), len(s.localCommitListeners))
		}
	})
	for _, mode := range []string{"source87-stored", "source105-memory"} {
		t.Run(mode, func(t *testing.T) {
			memory := mode == "source105-memory"
			s, store, def, owner := sourceDocumentObserverSession(t, memory)
			initial, want := 1, 3
			if memory {
				initial, want = 0, 5
			}
			if err := sourceDocumentSet(s, def, owner, initial, false); err != nil {
				t.Fatal(err)
			}
			before, err := store.Snapshot(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var inStorage atomic.Bool
			var storageCalls atomic.Int64
			original := store.appendFrame
			store.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
				if kind == 2 {
					storageCalls.Add(1)
					inStorage.Store(true)
					defer inStorage.Store(false)
				}
				return original(kind, ordinal, high, payload)
			}
			defer func() { store.appendFrame = original }()
			var frames []rawWatchFrame
			var callbackDuringStorage bool
			o, baseline, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
				advance: func(_ context.Context, frame rawWatchFrame) error {
					callbackDuringStorage = callbackDuringStorage || inStorage.Load()
					frames = append(frames, frame)
					return nil
				},
				end: func(WatchEnd) error { return nil },
			})
			if err != nil || !ok {
				t.Fatalf("observe: exists=%v err=%v", ok, err)
			}
			defer o.detach()
			if baseline["count"] != json.Number(string(rune('0'+initial))) {
				t.Fatalf("initial: %v err=%v", baseline, err)
			}
			if err := sourceDocumentSet(s, def, owner, want, memory); err != nil {
				t.Fatal(err)
			}
			wantOps := []Operation{{"s", []any{"count"}, json.Number(string(rune('0' + want)))}}
			if len(frames) != 1 || frames[0].Retired || frames[0].Value["count"] != json.Number(string(rune('0'+want))) || !reflect.DeepEqual(frames[0].Ops, wantOps) || callbackDuringStorage {
				t.Fatalf("source advance: %+v, want count=%d ops=%v outside storage; inStorage=%v", frames, want, wantOps, callbackDuringStorage)
			}
			// Source Observation.value is the acquisition value, not a mutable
			// current-value accessor. Go returns a detached copy of that value.
			baseline["count"] = -1
			o.mu.Lock()
			current := o.value["count"]
			o.mu.Unlock()
			if current != json.Number(string(rune('0'+want))) {
				t.Fatalf("baseline mutation reached internal value: %v", current)
			}
			if memory {
				after, err := store.Snapshot(context.Background())
				if err != nil || after.Seq != before.Seq || storageCalls.Load() != 0 {
					t.Fatalf("memory update stored early: before=%d after=%d calls=%d err=%v", before.Seq, after.Seq, storageCalls.Load(), err)
				}
				if err := s.UnloadDocuments(context.Background()); err != nil {
					t.Fatal(err)
				}
				if len(frames) != 1 || storageCalls.Load() != 1 {
					t.Fatalf("flush duplicated memory advance or missed storage: frames=%d calls=%d", len(frames), storageCalls.Load())
				}
			} else if storageCalls.Load() != 1 {
				t.Fatalf("stored advance did not call storage exactly once: %d", storageCalls.Load())
			}
		})
	}
}

func TestMainSynchronousDocumentObserverGuardAndLifetime(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, false)
	if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	failure := errors.New("observer failed")
	var ends []WatchEnd
	var reports []error
	var peer int
	bad, _, ok, err := s.observeDocumentDefinition(ctx, def, owner, nil, sessionDocumentObserver{
		advance: func(context.Context, rawWatchFrame) error { return failure },
		end:     func(end WatchEnd) error { ends = append(ends, end); return nil },
		report: func(_ context.Context, err error, info ReportInfo) {
			if info.Type != "listener_error" {
				t.Error(info)
			}
			reports = append(reports, err)
		},
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	good, _, ok, err := s.observeDocumentDefinition(ctx, def, owner, nil, sessionDocumentObserver{
		advance: func(context.Context, rawWatchFrame) error { peer++; return nil },
		end:     func(WatchEnd) error { return nil },
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if !s.documentPinned(good.id) {
		t.Fatal("attached observation did not pin incarnation")
	}
	if err := sourceDocumentSet(s, def, owner, 3, false); err != nil {
		t.Fatal("observer failure escaped into commit", err)
	}
	if peer != 1 || len(reports) != 1 || reports[0] != failure || len(ends) != 1 || ends[0].Reason != "listener_error" || ends[0].Error != failure {
		t.Fatalf("guard: peer=%d reports=%v endings=%+v", peer, reports, ends)
	}
	bad.detach()
	bad.mu.Lock()
	retained := bad.session != nil || bad.listener != nil || bad.observer.advance != nil || bad.observer.end != nil || bad.observer.report != nil || bad.value != nil
	bad.mu.Unlock()
	if retained {
		t.Fatal("detached guard retained host callbacks or source")
	}
	if s.failure() != nil {
		t.Fatal("observer error failed the Session", s.failure())
	}
	id := good.id
	good.detach()
	good.detach()
	if s.documentPinned(id) {
		t.Fatal("last detach retained incarnation pin")
	}
	if err := sourceDocumentSet(s, def, owner, 4, false); err != nil {
		t.Fatal(err)
	}
	if peer != 1 || len(ends) != 1 {
		t.Fatalf("detached observer called again: peer=%d endings=%v", peer, ends)
	}
}

func TestMainSynchronousDocumentObserverSameCommitDetach(t *testing.T) {
	for _, mode := range []string{"self", "peer"} {
		t.Run(mode, func(t *testing.T) {
			s, _, def, owner := sourceDocumentObserverSession(t, false)
			if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
				t.Fatal(err)
			}
			var first, second *sessionDocumentObservation
			var order []string
			first, _, _, _ = s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
				advance: func(_ context.Context, frame rawWatchFrame) error {
					order = append(order, "first")
					if frame.Retired {
						if mode == "self" {
							first.detach()
						} else {
							second.detach()
						}
					}
					return nil
				},
				end: func(WatchEnd) error { return nil },
			})
			second, _, _, _ = s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
				advance: func(context.Context, rawWatchFrame) error { order = append(order, "second"); return nil },
				end:     func(WatchEnd) error { return nil },
			})
			if first == nil || second == nil {
				t.Fatal("observations not attached")
			}
			defer first.detach()
			defer second.detach()
			_, err := s.Commit(context.Background(), func(tx *Tx) (any, error) {
				if err := tx.RetireDefinition(def, owner, nil); err != nil {
					return nil, err
				}
				doc, err := tx.AcquireDocument(def, owner, nil, nil)
				if err != nil {
					return nil, err
				}
				return nil, doc.ApplyOperations([]Operation{{"s", []any{"count"}, 2}})
			})
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"first", "second", "second"}
			if mode == "peer" {
				want = []string{"first", "first"}
			}
			if !reflect.DeepEqual(order, want) {
				t.Fatalf("same-commit detach order: got %v want %v", order, want)
			}
			detached := first
			if mode == "peer" {
				detached = second
			}
			detached.mu.Lock()
			retained := detached.session != nil || detached.id != 0 || detached.value != nil
			detached.mu.Unlock()
			if retained {
				t.Fatal("detached observer retained a same-commit recreation pin or value")
			}
		})
	}
}

func TestMainSynchronousDocumentObserverAdmissionAndClose(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, false)
	if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
		t.Fatal(err)
	}
	observer := sessionDocumentObserver{advance: func(context.Context, rawWatchFrame) error { return nil }, end: func(WatchEnd) error { return nil }}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if o, _, ok, err := s.observeDocumentDefinition(cancelled, def, owner, nil, observer); o != nil || ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled acquire: observation=%v ok=%v err=%v", o, ok, err)
	}
	var endings []WatchEnd
	observer.end = func(end WatchEnd) error { endings = append(endings, end); return nil }
	o, _, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, observer)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(endings) != 1 || endings[0].Reason != "failed" || endings[0].Error == nil || endings[0].Error.Error() != "session closed" {
		t.Fatalf("close endings: %+v", endings)
	}
	o.mu.Lock()
	retained := o.session != nil || o.listener != nil || o.value != nil
	o.mu.Unlock()
	if retained {
		t.Fatal("close retained active guard state")
	}
}

// The observer registry and definition-cache pins use different locks. Exercise
// concurrent detach against a publication without requiring a race-enabled run.
func TestMainSynchronousDocumentObserverConcurrentDetach(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, false)
	if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	o, _, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
		advance: func(context.Context, rawWatchFrame) error { close(entered); <-release; return nil },
		end:     func(WatchEnd) error { return nil },
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	var workers sync.WaitGroup
	workers.Add(1)
	done := make(chan error, 1)
	go func() { defer workers.Done(); done <- sourceDocumentSet(s, def, owner, 2, false) }()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		close(release)
		workers.Wait()
		t.Fatal("observer did not start")
	}
	o.detach()
	close(release)
	workers.Wait()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// Cancel exactly after registration, mirroring #attachDocument's post-attach
// cancellation check without sleeping or exposing a production test hook.
type sourceDocumentCancelAfterRegistration struct {
	context.Context
	cancel context.CancelFunc
	s      *Session
}

func (c *sourceDocumentCancelAfterRegistration) Err() error {
	if c.s.observerMu.TryLock() {
		registered := len(c.s.documentObservers) != 0
		c.s.observerMu.Unlock()
		if registered {
			c.cancel()
		}
	}
	return c.Context.Err()
}

func TestMainSynchronousDocumentObserverCancelledAfterRegistration(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, false)
	if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
		t.Fatal(err)
	}
	base, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx := &sourceDocumentCancelAfterRegistration{Context: base, cancel: cancel, s: s}
	calls := 0
	observation, initial, ok, err := s.observeDocumentDefinition(ctx, def, owner, nil, sessionDocumentObserver{
		advance: func(context.Context, rawWatchFrame) error { calls++; return nil },
		end:     func(WatchEnd) error { calls++; return nil },
	})
	if observation != nil || initial != nil || ok || !errors.Is(err, context.Canceled) {
		t.Fatalf("post-registration cancellation: observation=%v initial=%v ok=%v err=%v", observation, initial, ok, err)
	}
	s.observerMu.Lock()
	observers, listeners := len(s.documentObservers), len(s.localCommitListeners)
	s.observerMu.Unlock()
	if observers != 0 || listeners != 0 || calls != 0 {
		t.Fatalf("cancelled attachment retained state: observers=%d listeners=%d calls=%d", observers, listeners, calls)
	}
}

func TestMainSynchronousDocumentObserverCapacityAndRetention(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, false)
	if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
		t.Fatal(err)
	}
	observer := sessionDocumentObserver{advance: func(context.Context, rawWatchFrame) error { return nil }, end: func(WatchEnd) error { return nil }}
	capacity := s.observerCapacity()
	handles := make([]*sessionDocumentObservation, 0, capacity)
	for i := 0; i < capacity; i++ {
		handle, _, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, observer)
		if err != nil || !ok {
			t.Fatalf("attach %d: exists=%v err=%v", i, ok, err)
		}
		handles = append(handles, handle)
	}
	t.Cleanup(func() {
		for _, handle := range handles {
			handle.detach()
		}
	})
	if handle, initial, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, observer); handle != nil || initial != nil || ok || err == nil {
		t.Fatalf("capacity not enforced: handle=%v initial=%v exists=%v err=%v", handle, initial, ok, err)
	}
	id := handles[0].id
	handles[0].detach()
	if !s.documentPinned(id) {
		t.Fatal("detaching one observer unpinned its peers")
	}
	replacement, _, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, observer)
	if err != nil || !ok {
		t.Fatalf("capacity not reusable: exists=%v err=%v", ok, err)
	}
	handles = append(handles, replacement)
	for _, handle := range handles {
		handle.detach()
	}
	if s.documentPinned(id) {
		t.Fatal("last detach retained the document pin")
	}
	s.observerMu.Lock()
	reserved, count := s.observerReservedBytesLocked(), s.observerProcessCountLocked()
	s.observerMu.Unlock()
	if reserved != 0 || count != 0 {
		t.Fatalf("last detach retained quota/capacity: bytes=%d count=%d", reserved, count)
	}
}

// The caller may retain the acquisition value, but the Session-owned guard must
// release the retired incarnation even when the observation remains attached.
func TestMainSynchronousDocumentObserverRetirementValueRelease(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, false)
	if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
		t.Fatal(err)
	}
	var frames []rawWatchFrame
	observation, baseline, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
		advance: func(_ context.Context, frame rawWatchFrame) error { frames = append(frames, frame); return nil },
		end:     func(WatchEnd) error { return nil },
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	defer observation.detach()
	oldID := observation.id
	_, err = s.Commit(context.Background(), func(tx *Tx) (any, error) { return nil, tx.RetireDefinition(def, owner, nil) })
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 1 || !frames[0].Retired || frames[0].Value["count"] != json.Number("1") || len(frames[0].Ops) != 0 {
		t.Fatalf("retirement delivery: %+v", frames)
	}
	observation.mu.Lock()
	retainedID, retainedValue, attached := observation.id, observation.value, observation.session != nil
	observation.mu.Unlock()
	if retainedID != 0 || retainedValue != nil || !attached || s.documentPinned(oldID) {
		t.Fatalf("retired incarnation retained: id=%d value=%v attached=%v pinned=%v", retainedID, retainedValue, attached, s.documentPinned(oldID))
	}
	if baseline["count"] != json.Number("1") {
		t.Fatalf("caller-owned baseline changed: %v", baseline)
	}
	if err := sourceDocumentSet(s, def, owner, 2, false); err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || frames[1].Retired || len(frames[1].Ops) != 1 || frames[1].Ops[0][0] != "r" || frames[1].Value["count"] != json.Number("2") {
		t.Fatalf("recreation delivery: %+v", frames)
	}
	observation.mu.Lock()
	newID := observation.id
	observation.mu.Unlock()
	if newID == 0 || newID == oldID || !s.documentPinned(newID) {
		t.Fatalf("recreation pin: old=%d new=%d pinned=%v", oldID, newID, s.documentPinned(newID))
	}
}

// Source241/254: an attachment queued behind a commit sees its publication;
// a cancelled queued attachment returns without registering an observer.
func TestMainSynchronousDocumentObserverQueuedAttachment(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		name := "after-commit"
		if cancelled {
			name = "cancelled"
		}
		t.Run(name, func(t *testing.T) {
			s, _, def, owner := sourceDocumentObserverSession(t, false)
			if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			commitDone := make(chan error, 1)
			go func() {
				_, err := s.Commit(context.Background(), func(tx *Tx) (any, error) {
					doc, err := tx.AcquireDocument(def, owner, nil, nil)
					if err != nil {
						return nil, err
					}
					if err := doc.ApplyOperations([]Operation{{"s", []any{"count"}, 11}}); err != nil {
						return nil, err
					}
					close(entered)
					<-release
					return nil, nil
				})
				commitDone <- err
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				unblock()
				t.Fatal("commit did not enter")
			}
			base, cancel := context.WithCancel(context.Background())
			defer cancel()
			admission := &mainCommitAdmissionContext{Context: base, entered: make(chan struct{})}
			type result struct {
				observation *sessionDocumentObservation
				initial     JSON
				ok          bool
				err         error
			}
			resultCh := make(chan result, 1)
			calls := make(chan struct{}, 1)
			go func() {
				observation, initial, ok, err := s.observeDocumentDefinition(admission, def, owner, nil, sessionDocumentObserver{
					advance: func(context.Context, rawWatchFrame) error { calls <- struct{}{}; return nil },
					end:     func(WatchEnd) error { return nil },
				})
				resultCh <- result{observation, initial, ok, err}
			}()
			select {
			case <-admission.entered:
			case <-time.After(3 * time.Second):
				unblock()
				t.Fatal("attachment never attempted admission")
			}
			if cancelled {
				cancel()
			}
			unblock()
			if err := <-commitDone; err != nil {
				t.Fatal(err)
			}
			got := <-resultCh
			if cancelled {
				if got.observation != nil || got.initial != nil || got.ok || !errors.Is(got.err, context.Canceled) {
					t.Fatalf("cancelled queued attachment: %+v", got)
				}
			} else {
				if got.err != nil || !got.ok || got.observation == nil || got.initial["count"] != json.Number("11") {
					t.Fatalf("queued baseline: %+v", got)
				}
				got.observation.detach()
			}
			if len(calls) != 0 {
				t.Fatal("queued attachment received the earlier commit as a callback")
			}
			s.observerMu.Lock()
			registered := len(s.documentObservers)
			s.observerMu.Unlock()
			if registered != 0 {
				t.Fatalf("queued attachment retained %d observers", registered)
			}
		})
	}
}

// A document observation occupies its registration position in the local Set.
// A prior local listener can detach it from a copied set before its turn, while
// a listener attached afterwards observes the document callback first.
func TestMainSynchronousDocumentObserverLocalRegistrationOrder(t *testing.T) {
	for _, mode := range []string{"document-first", "local-first-detaches-document"} {
		t.Run(mode, func(t *testing.T) {
			s, _, def, owner := sourceDocumentObserverSession(t, false)
			if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
				t.Fatal(err)
			}
			var order []string
			var observation *sessionDocumentObservation
			attachDocument := func() {
				var ok bool
				var err error
				observation, _, ok, err = s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
					advance: func(context.Context, rawWatchFrame) error { order = append(order, "document"); return nil },
					end:     func(WatchEnd) error { return nil },
				})
				if err != nil || !ok {
					t.Fatal(ok, err)
				}
			}
			attachLocal := func() func() {
				detach, err := s.followSessionCommits(&sessionCommitListener{callback: func(context.Context, sessionObservedCommit) error {
					order = append(order, "local")
					if mode == "local-first-detaches-document" {
						observation.detach()
					}
					return nil
				}})
				if err != nil {
					t.Fatal(err)
				}
				return detach
			}
			var detachLocal func()
			if mode == "document-first" {
				attachDocument()
				detachLocal = attachLocal()
			} else {
				detachLocal = attachLocal()
				attachDocument()
			}
			defer observation.detach()
			defer detachLocal()
			if err := sourceDocumentSet(s, def, owner, 2, false); err != nil {
				t.Fatal(err)
			}
			want := []string{"document", "local"}
			if mode != "document-first" {
				want = []string{"local"}
			}
			if !reflect.DeepEqual(order, want) {
				t.Fatalf("registration order: got %v want %v", order, want)
			}
		})
	}
}

func TestMainSynchronousDocumentObserverFailureCause(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, false)
	if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
		t.Fatal(err)
	}
	endings := make(chan WatchEnd, 2)
	observation, _, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
		advance: func(context.Context, rawWatchFrame) error { t.Error("advance after failure"); return nil },
		end:     func(end WatchEnd) error { endings <- end; return nil },
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	cause := &StorageRejected{Reason: "document observer source failure"}
	if err := s.backendResult(context.Background(), cause, true); !errors.Is(err, cause) {
		t.Fatalf("failure result: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	closed, err := s.Closed(ctx)
	if err != nil || closed.Reason != "failed" || !errors.Is(closed.Error, cause) {
		t.Fatalf("closed: %+v err=%v", closed, err)
	}
	select {
	case end := <-endings:
		if end.Reason != "failed" || !errors.Is(end.Error, cause) {
			t.Fatalf("observer ending: %+v", end)
		}
	case <-ctx.Done():
		t.Fatal("observer never ended")
	}
	if len(endings) != 0 {
		t.Fatal("observer ended more than once")
	}
	observation.mu.Lock()
	retained := observation.session != nil || observation.value != nil || observation.listener != nil
	observation.mu.Unlock()
	if retained {
		t.Fatal("failed observer retained source/value/listener")
	}
}

// A terminal task record and retirement can appear in the same standalone
// publication. The document retirement precedes the observer's ended event.
// This does not exercise scheduler-backed observation, which is unsupported.
func TestMainSynchronousDocumentObserverTaskOwnerEnds(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	s, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	def, err := DefineDocument(DefinitionOptions{
		Kind: "observe.p06.task", Version: 1, Scope: "task",
		Initial:        func(JSON) (JSON, error) { return JSON{"count": 0}, nil },
		CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	var taskID ID
	_, err = s.Commit(context.Background(), func(tx *Tx) (any, error) {
		var err error
		taskID, err = tx.MintID()
		if err != nil {
			return nil, err
		}
		if err := tx.PutTask(Task{ID: taskID, Conversation: 1, Kind: "observe.p06.owner", Status: "running", Checkpoint: JSON{}}); err != nil {
			return nil, err
		}
		_, err = tx.AcquireDocument(def, taskID, nil, nil)
		return nil, err
	})
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	observation, _, ok, err := s.observeDocumentDefinition(context.Background(), def, taskID, nil, sessionDocumentObserver{
		advance: func(_ context.Context, frame rawWatchFrame) error {
			if !frame.Retired {
				t.Errorf("unexpected non-retirement frame: %+v", frame)
			}
			events = append(events, "retired")
			return nil
		},
		end: func(end WatchEnd) error {
			if end.Reason != "ended" || end.Error != nil {
				t.Errorf("owner ending: %+v", end)
			}
			events = append(events, "ended")
			return nil
		},
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	oldID := observation.id
	_, err = s.Commit(context.Background(), func(tx *Tx) (any, error) {
		if err := tx.RetireDefinition(def, taskID, nil); err != nil {
			return nil, err
		}
		return nil, tx.PutTask(Task{ID: taskID, Conversation: 1, Kind: "observe.p06.owner", Status: "done", Checkpoint: JSON{}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(events, []string{"retired", "ended"}) {
		t.Fatalf("owner termination order: %v", events)
	}
	if s.documentPinned(oldID) {
		t.Fatal("terminal owner retained document pin")
	}
	if next, initial, ok, err := s.observeDocumentDefinition(context.Background(), def, taskID, nil, sessionDocumentObserver{
		advance: func(context.Context, rawWatchFrame) error { return nil }, end: func(WatchEnd) error { return nil },
	}); err != nil || ok || next != nil || initial != nil {
		t.Fatalf("terminal owner attachment: handle=%v initial=%v ok=%v err=%v", next, initial, ok, err)
	}
}

func TestMainSynchronousDocumentObserverRejectsMalformedAddress(t *testing.T) {
	s, _, _, _ := sourceDocumentObserverSession(t, false)
	taskDef, err := DefineDocument(DefinitionOptions{
		Kind: "observe.p06.invalid-task", Version: 1, Scope: "task",
		Initial:        func(JSON) (JSON, error) { return JSON{}, nil },
		CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	observer := sessionDocumentObserver{advance: func(context.Context, rawWatchFrame) error { return nil }, end: func(WatchEnd) error { return nil }}
	key := "not-a-family"
	for _, tc := range []struct {
		name  string
		def   *DocumentDefinition
		owner ID
		key   *string
	}{
		{"nil-definition", nil, 0, nil},
		{"missing-task-scope", taskDef, 0, nil},
		{"unexpected-key", taskDef, 1, &key},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observation, initial, ok, err := s.observeDocumentDefinition(context.Background(), tc.def, tc.owner, tc.key, observer)
			if observation != nil || initial != nil || ok || err == nil {
				t.Fatalf("malformed address was not rejected: observation=%v initial=%v ok=%v err=%v", observation, initial, ok, err)
			}
		})
	}
	s.observerMu.Lock()
	defer s.observerMu.Unlock()
	if len(s.documentObservers) != 0 || len(s.localCommitListeners) != 0 {
		t.Fatal("malformed address registered an observer")
	}
}

// Source469: a throwing retirement observer gets no recreation in the same
// commit, releases its pin and does not prevent a healthy peer from advancing.
func TestMainSynchronousDocumentObserverRetirementErrorIsolation(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, false)
	if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("retirement callback failed")
	var calls, reports, endings int
	bad, _, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
		advance: func(_ context.Context, frame rawWatchFrame) error {
			calls++
			if !frame.Retired {
				t.Error("faulting observer received recreation")
			}
			return failure
		},
		end: func(end WatchEnd) error {
			endings++
			if end.Reason != "listener_error" || end.Error != failure {
				t.Errorf("unexpected ending: %+v", end)
			}
			return nil
		},
		report: func(_ context.Context, err error, info ReportInfo) {
			reports++
			if err != failure || info.Type != "listener_error" {
				t.Errorf("unexpected report: %v %+v", err, info)
			}
		},
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	defer bad.detach()
	var peer []rawWatchFrame
	good, _, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
		advance: func(_ context.Context, frame rawWatchFrame) error { peer = append(peer, frame); return nil },
		end:     func(WatchEnd) error { return nil },
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	defer good.detach()
	oldID := bad.id
	_, err = s.Commit(context.Background(), func(tx *Tx) (any, error) {
		if err := tx.RetireDefinition(def, owner, nil); err != nil {
			return nil, err
		}
		doc, err := tx.AcquireDocument(def, owner, nil, nil)
		if err != nil {
			return nil, err
		}
		return nil, doc.ApplyOperations([]Operation{{"s", []any{"count"}, 2}})
	})
	if err != nil {
		t.Fatalf("observer error escaped into commit: %v", err)
	}
	if calls != 1 || reports != 1 || endings != 1 || len(peer) != 2 || !peer[0].Retired || peer[1].Retired || peer[1].Value["count"] != json.Number("2") {
		t.Fatalf("guard isolation: calls=%d reports=%d endings=%d peer=%+v", calls, reports, endings, peer)
	}
	bad.mu.Lock()
	badID, retained := bad.id, bad.session != nil || bad.value != nil || bad.listener != nil
	bad.mu.Unlock()
	if retained || badID != 0 || s.documentPinned(oldID) {
		t.Fatalf("faulted observer retained old incarnation: id=%d retained=%v pinned=%v", badID, retained, s.documentPinned(oldID))
	}
	if err := sourceDocumentSet(s, def, owner, 3, false); err != nil || calls != 1 || len(peer) != 3 {
		t.Fatalf("later commit: err=%v bad calls=%d peer frames=%d", err, calls, len(peer))
	}
}

// Source guard reports before detaching; if the reporter detaches the observer,
// the guard must not deliver a second end event.
func TestMainSynchronousDocumentObserverReporterDetaches(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, false)
	if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("guard failure")
	var observation *sessionDocumentObservation
	var reports, endings int
	var ok bool
	var err error
	observation, _, ok, err = s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
		advance: func(context.Context, rawWatchFrame) error { return failure },
		end:     func(WatchEnd) error { endings++; return nil },
		report: func(_ context.Context, err error, _ ReportInfo) {
			if err != failure {
				t.Errorf("report cause: %v", err)
			}
			reports++
			observation.detach()
		},
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if err := sourceDocumentSet(s, def, owner, 2, false); err != nil {
		t.Fatal(err)
	}
	if reports != 1 || endings != 0 || s.failure() != nil {
		t.Fatalf("report/detach order: reports=%d endings=%d session=%v", reports, endings, s.failure())
	}
}

// Source215 throws from advance. A Go panic carrying an error is the closest
// synchronous adaptation; promise/thenable rejection remains separate.
func TestMainSynchronousDocumentObserverPanicIsolation(t *testing.T) {
	s, _, def, owner := sourceDocumentObserverSession(t, false)
	if err := sourceDocumentSet(s, def, owner, 1, false); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("observer panic")
	var reports []error
	var endings []WatchEnd
	var peer int
	bad, _, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
		advance: func(context.Context, rawWatchFrame) error { panic(failure) },
		end:     func(end WatchEnd) error { endings = append(endings, end); return nil },
		report:  func(_ context.Context, err error, _ ReportInfo) { reports = append(reports, err) },
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	defer bad.detach()
	good, _, ok, err := s.observeDocumentDefinition(context.Background(), def, owner, nil, sessionDocumentObserver{
		advance: func(context.Context, rawWatchFrame) error { peer++; return nil },
		end:     func(WatchEnd) error { return nil },
	})
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	defer good.detach()
	if err := sourceDocumentSet(s, def, owner, 2, false); err != nil {
		t.Fatalf("observer panic escaped into commit: %v", err)
	}
	if peer != 1 || len(reports) != 1 || reports[0] != failure || len(endings) != 1 || endings[0].Reason != "listener_error" || endings[0].Error != failure || s.failure() != nil {
		t.Fatalf("panic guard: peer=%d reports=%v endings=%+v session=%v", peer, reports, endings, s.failure())
	}
}
