package durable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func watchReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("deterministic watch delivery timed out")
		var zero T
		return zero
	}
}
func watchDoc(t *testing.T, s *Session) *DocumentDefinition {
	t.Helper()
	def := checkpointDef(t, DefinitionOptions{Kind: "watch.test", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"n": 0, "xs": []any{"a", "b"}}, nil }})
	checkpointCommit(t, s, func(tx *Tx) error { _, e := tx.AcquireDocument(def, 0, nil, nil); return e })
	return def
}
func watchAcquire(t *testing.T, s *Session, def *DocumentDefinition) *DocumentWatch {
	t.Helper()
	w, ok, e := s.WatchDefinition(bg, def, 0, nil)
	if e != nil || !ok {
		t.Fatal(w, ok, e)
	}
	return w
}
func watchSet(t *testing.T, s *Session, def *DocumentDefinition, n int) {
	t.Helper()
	checkpointCommit(t, s, func(tx *Tx) error {
		h, e := tx.AcquireDocument(def, 0, nil, nil)
		if e != nil {
			return e
		}
		return h.ApplyOperations([]Operation{{"s", []any{"n"}, n}})
	})
}
func TestObservationAtomicBaselineExactFramesOwnedAndOffLine(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, _ func() *Session) {
		def := watchDoc(t, s)
		w := watchAcquire(t, s, def)
		defer w.Stop()
		base, _ := w.Value()
		watchSet(t, s, def, 1)
		watchSet(t, s, def, 2)
		same, _ := w.Value()
		if !reflect.DeepEqual(base, same) {
			t.Fatal("advanced before start")
		}
		deliveries := make(chan int, 3)
		replica := any(map[string]any(base))
		if e := w.Start(func(ctx context.Context, value JSON, ops []Operation) error {
			var e error
			replica, e = ApplyOperations(replica, ops, s.limits)
			if e != nil {
				return e
			}
			owned, _ := ownJSONValue(value, s.limits)
			if !equalDeltaJSON(replica, owned) {
				t.Error("frame replay")
			}
			if _, e = s.Snapshot(ctx); e != nil {
				return e
			}
			n, _ := value["n"].(json.Number).Int64()
			value["n"] = 99
			ops[0][1].([]any)[0] = "mutation"
			deliveries <- int(n)
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		if watchReceive(t, deliveries) != 1 || watchReceive(t, deliveries) != 2 {
			t.Fatal("frame order")
		}
		latest, _ := w.Value()
		if latest["n"].(json.Number) != "2" {
			t.Fatal("callback authority")
		}
	})
}
func TestObservationSerialCallbacksOverflowAndRetirement(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, _ func() *Session) {
		def := watchDoc(t, s)
		w := watchAcquire(t, s, def)
		entered := make(chan struct{})
		release := make(chan struct{})
		delivered := make(chan WatchFrame, 3)
		if e := w.Start(func(ctx context.Context, value JSON, ops []Operation) error {
			delivered <- WatchFrame{Value: value, Ops: ops}
			if value != nil && value["n"].(json.Number) == "1" {
				close(entered)
				<-release
			}
			return nil
		}); e != nil {
			t.Fatal(e)
		}
		watchSet(t, s, def, 1)
		watchReceive(t, entered)
		for i := 2; i <= 102; i++ {
			watchSet(t, s, def, i)
		}
		// Only the pending101 frames collapse; active1 is not folded into reset.
		close(release)
		first := watchReceive(t, delivered)
		second := watchReceive(t, delivered)
		if first.Value["n"].(json.Number) != "1" || second.Value["n"].(json.Number) != "102" || second.Ops[0][0] != "r" {
			t.Fatal("overflow ordering", first, second)
		}
		checkpointCommit(t, s, func(tx *Tx) error { return tx.RetireDefinition(def, 0, nil) })
		terminal := watchReceive(t, delivered)
		if terminal.Value != nil || terminal.Ops[0][0] != "r" {
			t.Fatal("retirement")
		}
		watchReceive(t, w.Closed())
		end, _ := w.End()
		if end.Reason != "retired" {
			t.Fatal(end)
		}
		checkpointCommit(t, s, func(tx *Tx) error { _, e := tx.AcquireDocument(def, 0, nil, nil); return e })
		end, _ = w.End()
		if end.Reason != "retired" {
			t.Fatal("followed recreation")
		}
	})
}
func TestObservationMigrationVersionResetStateAndUnload(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, _ func() *Session) {
		old := watchDoc(t, s)
		oldWatch := watchAcquire(t, s, old)
		defer oldWatch.Stop()
		current := checkpointDef(t, DefinitionOptions{Kind: "watch.test", Version: 2, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{}, nil }, Migrate: func(v JSON, _ uint64) (JSON, error) { v["migrated"] = true; return v, nil }})
		before := checkpointSnapshot(t, s)
		newWatch := watchAcquire(t, s, current)
		defer newWatch.Stop()
		state, ok, e := s.DocumentState(bg, current, 0, nil)
		if e != nil || !ok {
			t.Fatal(e)
		}
		defer state.Dispose()
		if !reflect.DeepEqual(before, checkpointSnapshot(t, s)) {
			t.Fatal("watch hydration wrote")
		}
		oldFrames := make(chan []Operation, 2)
		newFrames := make(chan int, 2)
		oldWatch.Start(func(_ context.Context, _ JSON, ops []Operation) error { oldFrames <- ops; return nil })
		newWatch.Start(func(_ context.Context, v JSON, _ []Operation) error {
			n, _ := v["n"].(json.Number).Int64()
			newFrames <- int(n)
			return nil
		})
		checkpointCommit(t, s, func(tx *Tx) error { _, e := tx.AcquireDocument(current, 0, nil, nil); return e })
		if watchReceive(t, oldFrames)[0][0] != "r" {
			t.Fatal("older shape replacement")
		}
		if e = s.UnloadDocuments(bg); e != nil {
			t.Fatal(e)
		}
		watchSet(t, s, current, 4)
		// The later4 callback is an ordered drain barrier: a migration-only0
		// callback cannot race this assertion or disappear by dequeuing.
		if watchReceive(t, newFrames) != 4 {
			t.Fatal("migration-only delivery or unload loss")
		}
		v, e := state.Value(bg)
		if e != nil || v["n"].(json.Number) != "4" {
			t.Fatal("state value", v, e)
		}
		state.Dispose()
		if _, e = state.Value(bg); !errors.Is(e, ErrClosed) {
			t.Fatal("disposed state", e)
		}
	})
}
func TestObservationStopCancelCloseNoJoinAndListenerFailure(t *testing.T) {
	store, e := OpenMemory(MemoryOptions{})
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenSession(store)
	if e != nil {
		t.Fatal(e)
	}
	def := watchDoc(t, s)
	w := watchAcquire(t, s, def)
	entered := make(chan struct{})
	release := make(chan struct{})
	deliveryContext := make(chan context.Context, 1)
	w.Start(func(ctx context.Context, _ JSON, _ []Operation) error {
		deliveryContext <- ctx
		close(entered)
		<-release
		return nil
	})
	type watchContextKey struct{}
	producer, cancel := context.WithCancel(context.WithValue(bg, watchContextKey{}, "context-owned"))
	if _, e = s.Commit(producer, func(tx *Tx) error {
		h, e := tx.AcquireDocument(def, 0, nil, nil)
		if e != nil {
			return e
		}
		return h.Set(JSON{"n": 1, "xs": []any{}})
	}); e != nil {
		t.Fatal(e)
	}
	watchReceive(t, entered)
	actual := watchReceive(t, deliveryContext)
	cancel()
	if actual.Value(watchContextKey{}) != "context-owned" || actual.Err() != nil || actual.Done() != nil {
		t.Fatal("actual producer context lost values/cancellation", actual.Value(watchContextKey{}), actual.Err())
	}
	done := make(chan error, 1)
	go func() { done <- s.Close(bg) }()
	if e = watchReceive(t, done); e != nil {
		t.Fatal(e)
	}
	watchReceive(t, w.Closed())
	end, _ := w.End()
	if end.Reason != "session_closed" {
		t.Fatal(end)
	}
	close(release)
	if e = w.Start(func(context.Context, JSON, []Operation) error { return nil }); !errors.Is(e, ErrClosed) {
		t.Fatal("late start", e)
	}
	store, _ = OpenMemory(MemoryOptions{})
	s, _ = OpenSession(store)
	defer s.Close(bg)
	def = watchDoc(t, s)
	failed := watchAcquire(t, s, def)
	healthy := watchAcquire(t, s, def)
	defer healthy.Stop()
	got := make(chan struct{}, 1)
	failed.Start(func(context.Context, JSON, []Operation) error { return errors.New("listener failed") })
	healthy.Start(func(context.Context, JSON, []Operation) error { got <- struct{}{}; return nil })
	watchSet(t, s, def, 1)
	watchReceive(t, failed.Closed())
	watchReceive(t, got)
	end, _ = failed.End()
	if end.Reason != "listener_error" {
		t.Fatal(end)
	}
	cancelled, cancelWatch := context.WithCancel(bg)
	cw, ok, e := s.WatchDefinition(cancelled, def, 0, nil)
	if e != nil || !ok {
		t.Fatal(e)
	}
	cancelWatch()
	watchReceive(t, cw.Closed())
	end, _ = cw.End()
	if end.Reason != "cancelled" {
		t.Fatal(end)
	}
}
func TestObservationNoPublicationBeforeAdoptionOrOnRejection(t *testing.T) {
	store, e := OpenMemory(MemoryOptions{})
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenSession(store)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(bg)
	def := watchDoc(t, s)
	w := watchAcquire(t, s, def)
	defer w.Stop()
	delivered := make(chan struct{}, 1)
	w.Start(func(context.Context, JSON, []Operation) error { delivered <- struct{}{}; return nil })
	entered := make(chan struct{})
	release := make(chan struct{})
	store.appendFrame = func(byte, uint64, uint64, []byte) error { close(entered); <-release; return nil }
	done := make(chan error, 1)
	go func() {
		_, e := s.Commit(bg, func(tx *Tx) error {
			h, e := tx.AcquireDocument(def, 0, nil, nil)
			if e != nil {
				return e
			}
			return h.Set(JSON{"n": 1, "xs": []any{}})
		})
		done <- e
	}()
	watchReceive(t, entered)
	select {
	case <-delivered:
		t.Fatal("speculative publication")
	default:
	}
	close(release)
	if e = watchReceive(t, done); e != nil {
		t.Fatal(e)
	}
	watchReceive(t, delivered)
	store.appendFrame = func(byte, uint64, uint64, []byte) error { return reject("preadmission") }
	if _, e = s.Commit(bg, func(tx *Tx) error {
		h, e := tx.AcquireDocument(def, 0, nil, nil)
		if e != nil {
			return e
		}
		return h.Set(JSON{"n": 2, "xs": []any{}})
	}); e == nil {
		t.Fatal("rejection missing")
	}
	select {
	case <-delivered:
		t.Fatal("rejected publication")
	default:
	}
	store.appendFrame = func(byte, uint64, uint64, []byte) error { return errors.New("uncertain") }
	if _, e = s.Commit(bg, func(tx *Tx) error {
		h, e := tx.AcquireDocument(def, 0, nil, nil)
		if e != nil {
			return e
		}
		return h.Set(JSON{"n": 3, "xs": []any{}})
	}); !errors.Is(e, ErrPoisoned) {
		t.Fatal(e)
	}
	select {
	case <-delivered:
		t.Fatal("uncertain publication")
	default:
	}
}
func TestObservationPublicationSubscriptionAndProcessBounds(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, _ func() *Session) {
		def := watchDoc(t, s)
		baseline, p, e := s.SubscribeCommits(bg)
		if e != nil {
			t.Fatal(e)
		}
		defer p.Stop()
		watchSet(t, s, def, 1)
		frames := make(chan PublicationFrame, 2)
		p.Start(func(ctx context.Context, f PublicationFrame) error {
			if _, e := s.Snapshot(ctx); e != nil {
				return e
			}
			frames <- f
			return nil
		})
		frame := watchReceive(t, frames)
		if frame.Publication.Seq != baseline.Seq+1 || len(frame.Publication.Documents) != 1 {
			t.Fatal("publication", frame)
		}
		frame.Publication.Documents[0].Value["n"] = 999
		v, _, e := s.SnapshotDefinition(bg, def, 0, nil)
		if e != nil || v["n"].(json.Number) != "1" {
			t.Fatal("publication mutation", e)
		}
		list := []*DocumentWatch{}
		for i := 0; i < s.observerCapacity()-1; i++ {
			list = append(list, watchAcquire(t, s, def))
		}
		defer func() {
			for _, w := range list {
				w.Stop()
			}
		}()
		if _, _, e = s.WatchDefinition(bg, def, 0, nil); e == nil {
			t.Fatal("observer process bound")
		}
	})
}
func TestObservationCallbackInitiatesCommitAndStructuralFrame(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, _ func() *Session) {
		def := watchDoc(t, s)
		w := watchAcquire(t, s, def)
		defer w.Stop()
		done := make(chan struct{}, 1)
		var once sync.Once
		w.Start(func(_ context.Context, v JSON, _ []Operation) error {
			if v["n"].(json.Number) == "1" {
				once.Do(func() { watchSet(t, s, def, 2) })
			} else {
				done <- struct{}{}
			}
			return nil
		})
		watchSet(t, s, def, 1)
		watchReceive(t, done)
		w.Stop()
		w = watchAcquire(t, s, def)
		defer w.Stop()
		frames := make(chan []Operation, 1)
		w.Start(func(_ context.Context, _ JSON, ops []Operation) error { frames <- ops; return nil })
		checkpointCommit(t, s, func(tx *Tx) error {
			h, e := tx.AcquireDocument(def, 0, nil, nil)
			if e != nil {
				return e
			}
			return h.ApplyOperations([]Operation{{"p", []any{"xs"}, 0, 1, []any{}}, {"p", []any{"xs"}, 0, 0, []any{"a"}}})
		})
		if len(watchReceive(t, frames)) == 0 {
			t.Fatal("structural frame suppressed")
		}
	})
}

func TestObservationAbsentAndStateIncarnationAfterReopen(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, reopen func() *Session) {
		def := checkpointDef(t, DefinitionOptions{Kind: "watch.absent", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }})
		before := checkpointSnapshot(t, s)
		if w, ok, e := s.WatchDefinition(bg, def, 0, nil); e != nil || ok || w != nil {
			t.Fatal("absent watch", w, ok, e)
		}
		if state, ok, e := s.DocumentState(bg, def, 0, nil); e != nil || ok || state != nil {
			t.Fatal("absent state", state, ok, e)
		}
		if !reflect.DeepEqual(before, checkpointSnapshot(t, s)) || len(s.watches) != 0 || len(s.subscriptions) != 0 {
			t.Fatal("absent acquisition creates/allocates/registers")
		}
		checkpointCommit(t, s, func(tx *Tx) error { _, e := tx.AcquireDocument(def, 0, nil, nil); return e })
		state, ok, e := s.DocumentState(bg, def, 0, nil)
		if e != nil || !ok {
			t.Fatal(e)
		}
		defer state.Dispose()
		watchSet(t, s, def, 1)
		checkpointCommit(t, s, func(tx *Tx) error {
			if e := tx.RetireDefinition(def, 0, nil); e != nil {
				return e
			}
			h, e := tx.AcquireDocument(def, 0, nil, nil)
			if e != nil {
				return e
			}
			return h.Set(JSON{"n": 2})
		})
		if value, e := state.Value(bg); e != nil || value != nil {
			t.Fatal("old state followed recreation", value, e)
		}
		s = reopen()
		current, ok, e := s.DocumentState(bg, def, 0, nil)
		if e != nil || !ok {
			t.Fatal(e)
		}
		defer current.Dispose()
		value, e := current.Value(bg)
		if e != nil || value["n"].(json.Number) != "2" {
			t.Fatal("reopen state", value, e)
		}
	})
}
func TestObservationInactiveWatchAndPublicSubscriptionOverflow101(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, _ func() *Session) {
		def := watchDoc(t, s)
		w := watchAcquire(t, s, def)
		defer w.Stop()
		_, p, e := s.SubscribeCommits(bg)
		if e != nil {
			t.Fatal(e)
		}
		defer p.Stop()
		for i := 1; i <= 101; i++ {
			watchSet(t, s, def, i)
		}
		values := make(chan int, 2)
		w.Start(func(_ context.Context, v JSON, ops []Operation) error {
			if len(ops) != 1 || ops[0][0] != "r" {
				t.Error("inactive watch101 not reset")
			}
			n, _ := v["n"].(json.Number).Int64()
			values <- int(n)
			return nil
		})
		frames := make(chan PublicationFrame, 2)
		p.Start(func(_ context.Context, f PublicationFrame) error { frames <- f; return nil })
		if watchReceive(t, values) != 101 {
			t.Fatal("inactive watch latest")
		}
		frame := watchReceive(t, frames)
		if frame.Snapshot == nil || frame.Publication.Seq != 0 {
			t.Fatal("PUBLIC subscription did not resnapshot101", frame)
		}
		for _, d := range frame.Snapshot.Documents {
			if d.Kind == "watch.test" && d.Value["n"].(json.Number) != "101" {
				t.Fatal("public snapshot stale", d)
			}
		}
	})
}
func TestObservationByteQuotaEarlyResetAcquireAndLaterTermination(t *testing.T) {
	l := DefaultLimits()
	l.MaxRetainedBytes = 64 << 10
	l.MaxPage = 4
	store, e := OpenMemory(MemoryOptions{Limits: &l})
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenSession(store)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(bg)
	text := strings.Repeat("x", 3000)
	def := checkpointDef(t, DefinitionOptions{Kind: "watch.bytes", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"n": 0, "text": text}, nil }})
	checkpointCommit(t, s, func(tx *Tx) error { _, e := tx.AcquireDocument(def, 0, nil, nil); return e })
	w := watchAcquire(t, s, def)
	defer w.Stop()
	for i := 1; i <= 3; i++ {
		checkpointCommit(t, s, func(tx *Tx) error {
			h, e := tx.AcquireDocument(def, 0, nil, nil)
			if e != nil {
				return e
			}
			return h.Set(JSON{"n": i, "text": text})
		})
	}
	frames := make(chan WatchFrame, 2)
	w.Start(func(_ context.Context, v JSON, ops []Operation) error {
		frames <- WatchFrame{Value: v, Ops: ops}
		return nil
	})
	first := watchReceive(t, frames)
	if first.Ops[0][0] != "r" || first.Value["n"].(json.Number) != "3" {
		t.Fatal("byte quota did not reset earlier than100", first)
	}
	// Migration-only acquisition cannot retain an oversized baseline, and does
	// not write or register a watch while computing that detached value.
	huge := checkpointDef(t, DefinitionOptions{Kind: "watch.bytes", Version: 2, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{}, nil }, Migrate: func(v JSON, _ uint64) (JSON, error) { v["text"] = strings.Repeat("y", 9000); return v, nil }})
	before := checkpointSnapshot(t, s)
	if _, ok, e := s.WatchDefinition(bg, huge, 0, nil); e == nil || ok {
		t.Fatal("overquota baseline accepted")
	}
	if !reflect.DeepEqual(before, checkpointSnapshot(t, s)) {
		t.Fatal("failed baseline wrote")
	}
	checkpointCommit(t, s, func(tx *Tx) error {
		h, e := tx.AcquireDocument(def, 0, nil, nil)
		if e != nil {
			return e
		}
		return h.Set(JSON{"n": 4, "text": strings.Repeat("z", 9000)})
	})
	watchReceive(t, w.Closed())
	end, _ := w.End()
	if end.Reason != "budget_exceeded" {
		t.Fatal("later quota terminal", end)
	}
}

func TestObservationRegistrationSnapshotAndCancellationBarrier(t *testing.T) {
	store, e := OpenMemory(MemoryOptions{})
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenSession(store)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(bg)
	old := watchDoc(t, s)
	entered := make(chan struct{})
	release := make(chan struct{})
	migrated := checkpointDef(t, DefinitionOptions{Kind: "watch.test", Version: 2, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{}, nil }, Migrate: func(v JSON, _ uint64) (JSON, error) { close(entered); <-release; v["migrated"] = true; return v, nil }})
	acquired := make(chan *DocumentWatch, 1)
	acquireError := make(chan error, 1)
	go func() {
		w, ok, e := s.WatchDefinition(bg, migrated, 0, nil)
		if !ok && e == nil {
			e = errors.New("missing")
		}
		acquired <- w
		acquireError <- e
	}()
	watchReceive(t, entered)
	started := make(chan struct{})
	committed := make(chan error, 1)
	go func() {
		close(started)
		_, e := s.Commit(bg, func(tx *Tx) error {
			h, e := tx.AcquireDocument(migrated, 0, nil, nil)
			if e != nil {
				return e
			}
			return h.Set(JSON{"n": 1, "xs": []any{"a", "b"}, "migrated": true})
		})
		committed <- e
	}()
	watchReceive(t, started)
	close(release)
	w := watchReceive(t, acquired)
	if e = watchReceive(t, acquireError); e != nil {
		t.Fatal(e)
	}
	defer w.Stop()
	if e = watchReceive(t, committed); e != nil {
		t.Fatal(e)
	}
	baseline, e := w.Value()
	if e != nil || baseline["n"].(json.Number) != "0" {
		t.Fatal("registration baseline", baseline, e)
	}
	values := make(chan int, 1)
	w.Start(func(_ context.Context, v JSON, _ []Operation) error {
		n, _ := v["n"].(json.Number).Int64()
		values <- int(n)
		return nil
	})
	if watchReceive(t, values) != 1 {
		t.Fatal("missed admitted change after acquisition")
	}
	_ = old
	// A cancelled acquisition during the same migration barrier registers none.
	another, e := OpenMemory(MemoryOptions{})
	if e != nil {
		t.Fatal(e)
	}
	s2, e := OpenSession(another)
	if e != nil {
		t.Fatal(e)
	}
	defer s2.Close(bg)
	watchDoc(t, s2)
	gate := make(chan struct{})
	done := make(chan struct{})
	cancelDef := checkpointDef(t, DefinitionOptions{Kind: "watch.test", Version: 2, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{}, nil }, Migrate: func(v JSON, _ uint64) (JSON, error) { close(gate); <-done; return v, nil }})
	ctx, cancel := context.WithCancel(bg)
	result := make(chan error, 1)
	go func() { _, _, e := s2.WatchDefinition(ctx, cancelDef, 0, nil); result <- e }()
	watchReceive(t, gate)
	cancel()
	close(done)
	if e = watchReceive(t, result); !errors.Is(e, context.Canceled) {
		t.Fatal("cancel acquisition", e)
	}
	s2.observerMu.Lock()
	count := len(s2.watches)
	s2.observerMu.Unlock()
	if count != 0 {
		t.Fatal("cancelled acquisition leaked")
	}
}

func TestObservationPublicationCompleteAdoptedTablePlacementAndReopen(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, reopen func() *Session) {
		def := watchDoc(t, s)
		_, sub, e := s.SubscribeCommits(bg)
		if e != nil {
			t.Fatal(e)
		}
		defer sub.Stop()
		frames := make(chan PublicationFrame, 1)
		sub.Start(func(_ context.Context, f PublicationFrame) error { frames <- f; return nil })
		token := entryToken(t, "entry.publication")
		var first, second, taskID ID
		checkpointCommit(t, s, func(tx *Tx) error {
			h, e := tx.AcquireDocument(def, 0, nil, nil)
			if e != nil {
				return e
			}
			if e = h.ApplyOperations([]Operation{{"s", []any{"n"}, 1}}); e != nil {
				return e
			}
			a, e := tx.AppendTypedEntry(token, 1, EntryContent{Data: "a", HasData: true})
			if e != nil {
				return e
			}
			first = a.ID
			id, e := tx.MintID()
			if e != nil {
				return e
			}
			taskID = id
			if e = tx.PutTask(Task{ID: id, Conversation: 1, Kind: "publication.task", Status: "pending", Checkpoint: JSON{"phase": "start"}}); e != nil {
				return e
			}
			b, e := tx.AppendTypedEntry(token, 1, EntryContent{Data: "b", HasData: true})
			second = b.ID
			return e
		})
		frame := watchReceive(t, frames)
		if frame.Snapshot != nil || len(frame.Publication.Tables) != 3 || len(frame.Publication.Documents) != 1 {
			t.Fatal("mixed publication", frame)
		}
		verify := func(s *Session) {
			state := checkpointSnapshot(t, s)
			positions := map[ID]uint64{first: 2, second: 4}
			for _, write := range frame.Publication.Tables {
				if write.Entry != nil {
					want := state.Entries[write.Entry.ID]
					if write.Entry.Seq != frame.Publication.Seq || write.Entry.Position != positions[write.Entry.ID] || !reflect.DeepEqual(*write.Entry, want) {
						t.Fatal("adopted complete entry projection", write.Entry, want)
					}
				}
				if write.Task != nil && !reflect.DeepEqual(*write.Task, state.Tasks[taskID]) {
					t.Fatal("adopted task projection")
				}
			}
		}
		verify(s)
		s = reopen()
		verify(s)
	})
}
