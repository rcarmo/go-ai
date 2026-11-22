package durable

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Consolidated from session_batching_v150_test.go.

// Pinned test/session-group-commit.test.ts:170: disjoint callbacks are admitted
// during a held call, then the writer sends both waiting parts in one call.
func TestSessionV150DisjointDocumentAdmissionBatchesWaitingParts(t *testing.T) {
	for _, withHarness := range []bool{false, true} {
		name := "session"
		if withHarness {
			name = "harness"
		}
		t.Run(name, func(t *testing.T) {
			var s *Session
			var commitChange func(context.Context, func(*Tx) (any, error), ...CommitOptions) (any, error)
			if withHarness {
				store, err := newTestSQLite(t)
				if err != nil {
					t.Fatal(err)
				}
				h := taskTestHarness(t, store)
				s = h.session
				commitChange = h.Commit
			} else {
				s = gatedSession(t)
				commitChange = s.Commit
			}

			def, err := DefineDocument(DefinitionOptions{Kind: "batch.note", Scope: "conversation", History: "latest", Fork: "initial", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
			if err != nil {
				t.Fatal(err)
			}
			keys := []string{"a", "b", "c"}
			owners := []ID{1}
			if _, err = commitChange(bg, func(tx *Tx) (any, error) {
				for i := 0; i < 2; i++ {
					id, e := tx.mintID()
					if e != nil {
						return nil, e
					}
					if e := tx.createConversationRecord(Conversation{ID: id}); e != nil {
						return nil, e
					}
					owners = append(owners, id)
				}
				return nil, nil
			}); err != nil {
				t.Fatal(err)
			}
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			var mu sync.Mutex
			var calls []int
			commit := s.writer.host.commit
			s.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
				mu.Lock()
				calls = append(calls, len(batches))
				first := len(calls) == 1
				mu.Unlock()
				if first {
					close(entered)
					<-release
				}
				return commit(ctx, batches, receive)
			}
			var seqs []uint64
			observation, err := s.observeStoredCommits(sessionCommitObserver{advance: func(_ context.Context, p sessionPublication) error {
				mu.Lock()
				seqs = append(seqs, p.Seq)
				for _, change := range p.Documents {
					if change.Record.CreatedAt != p.Seq {
						t.Errorf("createdAt=%d, receipt=%d", change.Record.CreatedAt, p.Seq)
					}
				}
				mu.Unlock()
				return nil
			}, end: func(WatchEnd) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer observation.detach()
			results := make([]chan error, 3)
			ran := make(chan string, 3)
			for i := range keys {
				results[i] = make(chan error, 1)
				i := i
				go func() {
					_, e := commitChange(bg, func(tx *Tx) (any, error) {
						doc, e := tx.AcquireDocument(def, owners[i], nil, nil)
						if e != nil {
							return nil, e
						}
						if e = doc.Set(JSON{"n": 1}); e != nil {
							return nil, e
						}
						ran <- keys[i]
						return nil, nil
					})
					results[i] <- e
				}()
				if i == 0 {
					<-entered
					<-ran
					continue
				}
				select {
				case <-ran:
				case <-time.After(2 * time.Second):
					unblock()
					for j := 0; j <= i; j++ {
						<-results[j]
					}
					t.Fatal("disjoint callback blocked behind the held writer")
				}
			}
			// Queue a read after all three line parts, so its gate witnesses that both
			// later commits were admitted, not merely that their callbacks returned.
			queued := make(chan struct{})
			readDone := make(chan error, 1)
			go func() { _, e := s.Snapshot(&documentGateContext{Context: bg, queued: queued}); readDone <- e }()
			awaitTaskSignal(t, queued)
			mu.Lock()
			early := len(seqs)
			mu.Unlock()
			if early != 0 {
				t.Error("published before storage release")
			}
			unblock()
			for _, done := range results {
				if e := <-done; e != nil {
					t.Error(e)
				}
			}
			if e := <-readDone; e != nil {
				t.Error(e)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(calls, []int{1, 2}) {
				t.Errorf("calls %v, want [1 2]", calls)
			}
			if len(seqs) != 3 || seqs[1] != seqs[0]+1 || seqs[2] != seqs[1]+1 {
				t.Errorf("publication sequences %v", seqs)
			}
		})
	}
}

// Consolidated from session_entry_batching_v150_test.go.

// Transaction.#appendEntry only requires the conversation; it does not read its
// append key. Storage validates head references against the ordered batches.
func TestSessionV150BlindEntryAppendsBatchAgainstPendingPrefix(t *testing.T) {
	s := gatedSession(t)
	def, err := DefineEntry("blind.append")
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	var calls []int
	var mu sync.Mutex
	commit := s.writer.host.commit
	s.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		mu.Lock()
		calls = append(calls, len(batches))
		first := len(calls) == 1
		mu.Unlock()
		if first {
			close(entered)
			<-release
		}
		return commit(ctx, batches, receive)
	}
	results := make([]chan error, 3)
	var firstID ID
	for i := 0; i < 3; i++ {
		results[i] = make(chan error, 1)
		ran := make(chan struct{})
		i := i
		go func() {
			_, err := s.Commit(bg, func(tx *Tx) (any, error) {
				content := entryContentFixture{Value: JSON{"index": i}}
				if i > 0 {
					content.Head = firstID
				}
				entry, err := tx.appendTypedEntryFixture(def, 1, content)
				if i == 0 {
					firstID = entry.ID
				}
				close(ran)
				return nil, err
			})
			results[i] <- err
		}()
		if i == 0 {
			<-entered
			<-ran
			continue
		}
		select {
		case <-ran:
		case <-time.After(time.Second):
			unblock()
			for j := 0; j <= i; j++ {
				<-results[j]
			}
			t.Fatal("blind append waited for an unrelated append-key writer")
		}
	}
	queued := make(chan struct{})
	read := make(chan error, 1)
	go func() { _, err := s.Snapshot(&documentGateContext{Context: bg, queued: queued}); read <- err }()
	awaitTaskSignal(t, queued)
	unblock()
	for _, done := range results {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if err := <-read; err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]int(nil), calls...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatalf("calls=%v, want [1 2]", got)
	}
	state, err := s.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Entries) != 3 {
		t.Fatalf("entries=%d", len(state.Entries))
	}
	sequences := map[uint64]bool{}
	for _, entry := range state.Entries {
		sequences[entry.Seq] = true
		if entry.Position != 1 || entry.Seq < 2 || entry.Seq > 4 {
			t.Errorf("receipt-time placement %+v", entry)
		}
		if entry.ID != firstID && entry.Head != firstID {
			t.Errorf("head reference lost %+v", entry)
		}
	}
	if len(sequences) != 3 {
		t.Fatalf("duplicate receipt sequences %v", sequences)
	}
}

// Consolidated from session_document_keys_v150_test.go.

// Source: session.ts #loadDocument, snapshotAsOf, and keys.ts GatedStorage.
// Address reads execute behind only their dependencies; #enqueue still fences
// their outcomes behind preceding jobs. Cold tokens witness the read itself.
func TestSessionV150DocumentReadsTouchOnlyTheirDependencies(t *testing.T) {
	for _, historical := range []bool{false, true} {
		name := "current"
		if historical {
			name = "historical"
		}
		t.Run(name, func(t *testing.T) {
			s := gatedSession(t)
			definition := func(kind string) *DocumentDefinition {
				d, err := DefineDocument(DefinitionOptions{Kind: kind, Version: 1, Scope: "conversation", History: "rewindable", Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
				if err != nil {
					t.Fatal(err)
				}
				return d
			}
			a, b := definition("keyed.a"), definition("keyed.b")
			coldToken := func(original *DocumentDefinition) (*DocumentDefinition, <-chan struct{}) {
				ran := make(chan struct{})
				var once sync.Once
				options := original.options
				options.Version = 2
				options.Migrate = func(value JSON, _ uint64) (JSON, error) { once.Do(func() { close(ran) }); return value, nil }
				token, err := DefineDocument(options)
				if err != nil {
					t.Fatal(err)
				}
				return token, ran
			}
			readA, _ := coldToken(a)
			readB, ranB := coldToken(b)
			var at ID
			if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
				if _, err := tx.AcquireDocument(a, 1, nil, nil); err != nil {
					return nil, err
				}
				if _, err := tx.AcquireDocument(b, 1, nil, nil); err != nil {
					return nil, err
				}
				var err error
				at, err = tx.mintID()
				if err != nil {
					return nil, err
				}
				return nil, tx.appendEntryRecord(Entry{ID: at, Conversation: 1, Kind: "cutoff", Value: JSON{}})
			}); err != nil {
				t.Fatal(err)
			}

			entered, release := make(chan struct{}), make(chan struct{})
			released := false
			unblock := func() {
				if !released {
					released = true
					close(release)
				}
			}
			defer unblock()
			commit := s.writer.host.commit
			s.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
				close(entered)
				<-release
				return commit(ctx, batches, receive)
			}
			stored := make(chan error, 1)
			go func() {
				_, err := s.Commit(bg, func(tx *Tx) (any, error) {
					doc, err := tx.AcquireDocument(a, 1, nil, nil)
					if err != nil {
						return nil, err
					}
					return nil, doc.Set(JSON{"n": 1})
				})
				stored <- err
			}()
			<-entered
			read := func(ctx context.Context, def *DocumentDefinition) error {
				var err error
				var found bool
				var value JSON
				if historical {
					value, found, err = detachedDefinitionSnapshotAsOf(s, ctx, def, 1, nil, at)
				} else {
					value, found, err = detachedDefinitionSnapshot(s, ctx, def, 1, nil)
				}
				if err == nil && !found {
					return reject("document missing")
				}
				want := 0
				if def == readA && !historical {
					want = 1
				}
				n, ok := exactNumber(value["n"])
				if err == nil && (!ok || n == nil || !n.IsInt() || n.Num().Int64() != int64(want)) {
					return reject("wrong document value at requested point")
				}
				return err
			}
			disjoint := make(chan error, 1)
			go func() { disjoint <- read(bg, readB) }()
			select {
			case <-ranB:
			case <-time.After(2 * time.Second):
				unblock()
				<-stored
				<-disjoint
				t.Fatal("unrelated document read did not execute before storage release")
			}
			select {
			case err := <-disjoint:
				t.Fatalf("cold read outcome escaped its fence: %v", err)
			default:
			}
			dependent := make(chan error, 1)
			queued := make(chan struct{})
			ctx := &documentGateContext{Context: bg, queued: queued}
			go func() { dependent <- read(ctx, readA) }()
			// The fourth Done consultation is touch's select, after room and both queues.
			// The signal witnesses the actual gate wait, not goroutine launch.
			awaitTaskSignal(t, queued)
			select {
			case err := <-dependent:
				t.Fatalf("dependent read escaped before publication: %v", err)
			default:
			}
			unblock()
			if err := <-stored; err != nil {
				t.Fatal(err)
			}
			if err := <-dependent; err != nil {
				t.Fatal(err)
			}
			if err := <-disjoint; err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Room and two queue acquisitions precede the read gate. Signal only at the gate so
// launch/admission alone cannot satisfy a dependent-read assertion.
type documentGateContext struct {
	context.Context
	queued chan struct{}
	calls  atomic.Int64
}

func (c *documentGateContext) Done() <-chan struct{} {
	if c.calls.Add(1) == 4 {
		close(c.queued)
	}
	return c.Context.Done()
}

// Source session-group-commit:759, plus single-part scope/once/deferred controls
// derived from session.ts #publishing. Callback result is captured in Go rather
// than the source's generic Promise result. Storage and publication are real.
type v150PublicationScopeStorage struct {
	Storage
	core       *storeCore
	scope      func(context.Context, func())
	completion <-chan struct{}
}

func (w *v150PublicationScopeStorage) Snapshot(ctx context.Context) (Snapshot, error) {
	return testStorageProjection(w.Storage).Snapshot(ctx)
}
func (w *v150PublicationScopeStorage) nativeCore() *storeCore { return w.core }
func (w *v150PublicationScopeStorage) Publishing(ctx context.Context, publish func()) <-chan struct{} {
	w.scope(ctx, publish)
	return w.completion
}
func TestSessionV150StoragePublicationScope(t *testing.T) {
	for _, mode := range []string{"inside", "twice", "reentrant", "throw-before", "throw-after", "deferred"} {
		t.Run(mode, func(t *testing.T) {
			s := gatedSession(t)
			def, err := DefineDocument(DefinitionOptions{Kind: "scope.counter", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Commit(bg, func(tx *Tx) (any, error) { _, err := tx.AcquireDocument(def, 0, nil, nil); return nil, err }); err != nil {
				t.Fatal(err)
			}
			marker := errors.New("no scope")
			var reports []error
			s.timerReport = func(err error) { reports = append(reports, err) }
			depth, calls, published := 0, 0, 0
			var saved func()
			observer, err := s.observeStoredCommits(sessionCommitObserver{advance: func(context.Context, sessionPublication) error {
				published++
				if mode == "reentrant" {
					saved()
				}
				if mode != "throw-before" && depth != 1 {
					t.Error("published outside storage scope")
				}
				return nil
			}, end: func(WatchEnd) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer observer.detach()
			delayed := make(chan func(), 1)
			run := func(publish func()) {
				depth++
				defer func() { depth-- }()
				publish()
				if mode == "twice" {
					publish()
				}
				if mode == "throw-after" {
					panic(marker)
				}
			}
			s.store = &v150PublicationScopeStorage{Storage: s.store, core: s.core, scope: func(ctx context.Context, publish func()) {
				calls++
				saved = publish
				if ctx.Value(v150ScopeContextKey{}) != "producer" {
					t.Error("scope lost producer context")
				}
				if mode == "throw-before" {
					panic(marker)
				}
				if mode == "deferred" {
					delayed <- func() { run(publish) }
					return
				}
				run(publish)
			}}
			result := make(chan error, 1)
			var prior any
			go func() {
				_, err := s.Commit(context.WithValue(bg, v150ScopeContextKey{}, "producer"), func(tx *Tx) (any, error) {
					doc, err := tx.AcquireDocument(def, 0, nil, nil)
					if err != nil {
						return nil, err
					}
					value, err := doc.Get()
					if err != nil {
						return nil, err
					}
					prior = value["n"]
					return nil, doc.ApplyOperations([]Operation{{"s", []any{"n"}, 1}})
				})
				result <- err
			}()
			if mode == "deferred" {
				select {
				case publish := <-delayed:
					select {
					case err := <-result:
						t.Fatal("outcome before delayed publication", err)
					default:
					}
					publish()
				case err := <-result:
					t.Fatal("storage publication scope was ignored", err)
				case <-time.After(3 * time.Second):
					t.Fatal("scope not entered")
				}
			}
			if err := watchReceive(t, result); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || published != 1 {
				t.Fatal("scope/once counts", calls, published)
			}
			n, ok := exactNumber(prior)
			if !ok || n.Sign() != 0 {
				t.Fatal("callback result", prior)
			}
			if mode == "throw-before" || mode == "throw-after" {
				if len(reports) != 1 || reports[0] != marker {
					t.Fatal("scope failure identity", reports)
				}
			} else if len(reports) != 0 {
				t.Fatal(reports)
			}
			value, found, err := s.SnapshotDefinition(bg, def, 0, nil)
			if err != nil || !found || immutableViewMember(t, value, "n").Number() != "1" {
				t.Fatal("stored publication lost", found, err)
			}
		})
	}
}

type v150ScopeContextKey struct{}

// Source827 deferred-entry failure contract through the actual Storage scope.
// Go stores these two writes in separate calls (not the source's held group),
// while preserving failure-at-entry, durable-but-unpublished and cause checks.
func TestSessionV150StorageScopeRechecksFailureAtDeferredEntry(t *testing.T) {
	s := gatedSession(t)
	failure := errors.New("failed before a deferred publication ran")
	calls := 0
	delayed := make(chan func(), 1)
	s.store = &v150PublicationScopeStorage{Storage: s.store, core: s.core, scope: func(_ context.Context, publish func()) {
		calls++
		if calls == 2 {
			s.writerFail(failure, s.nextJobIndex, nil)
			delayed <- publish
			return
		}
		publish()
	}}
	var kinds []string
	observer, err := s.observeStoredCommits(sessionCommitObserver{advance: func(_ context.Context, p sessionPublication) error {
		for _, w := range p.Tables {
			if w.Entry != nil {
				kinds = append(kinds, w.Entry.Kind)
			}
		}
		return nil
	}, end: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer observer.detach()
	appendKind := func(kind string) error {
		_, err := s.Commit(bg, func(tx *Tx) (any, error) {
			id, err := tx.mintID()
			if err != nil {
				return nil, err
			}
			return nil, tx.appendEntryRecord(Entry{ID: id, Conversation: 1, Kind: kind, Value: JSON{}})
		})
		return err
	}
	if err := appendKind("lead"); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { result <- appendKind("one") }()
	publish := watchReceive(t, delayed)
	publish()
	var failed *SessionFailed
	if err := watchReceive(t, result); !errors.As(err, &failed) || failed.Cause != failure {
		t.Fatal("deferred failure identity", err)
	}
	if !reflect.DeepEqual(kinds, []string{"lead"}) {
		t.Fatal("published after scope-entry failure", kinds)
	}
	if calls != 2 {
		t.Fatal("scope calls", calls)
	}
}

// Source-derived transaction.ts348-418: ownerless construction has no committed
// read dependency and must not drain an unrelated unpublished entry write.
func TestSourceConversationConstructorAdmitsBlindCreatesDuringWriterFlight(t *testing.T) {
	store := taskConformanceStoreV150(t)
	session, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close(bg)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	original := session.writer.host.commit
	var mu sync.Mutex
	var calls []int
	session.writer.host.commit = func(ctx context.Context, batches []Batch, receive func(commitReceipt)) (commitGroupResult, error) {
		mu.Lock()
		calls = append(calls, len(batches))
		first := len(calls) == 1
		mu.Unlock()
		if first {
			close(entered)
			<-release
		}
		return original(ctx, batches, receive)
	}
	first := make(chan error, 1)
	go func() {
		_, err := session.Commit(bg, func(tx *Tx) (any, error) { return tx.AppendEntry(1, JSON{"kind": "source.held"}) })
		first <- err
	}()
	<-entered
	results := make([]chan error, 2)
	ran := make(chan ID, 2)
	for i := range results {
		results[i] = make(chan error, 1)
		result := results[i]
		go func() {
			_, err := session.Commit(bg, func(tx *Tx) (any, error) {
				record, err := tx.CreateConversation(JSON{"ownership": JSON{"kind": "ownerless"}})
				if err == nil {
					ran <- record.ID
				}
				return record, err
			})
			result <- err
		}()
	}
	identities := map[ID]bool{}
	for range 2 {
		select {
		case id := <-ran:
			if id == 0 || identities[id] {
				t.Fatal("blind constructor identity", id)
			}
			identities[id] = true
		case <-time.After(5 * time.Second):
			t.Fatal("ownerless constructor drained unrelated writer flight")
		}
	}
	// A later no-op admission callback proves both earlier preparations have
	// finished before releasing storage; callback-return timing is not a gate.
	barrierEntered, barrierResult := make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := session.Commit(bg, func(*Tx) (any, error) { close(barrierEntered); return nil, nil })
		barrierResult <- err
	}()
	select {
	case <-barrierEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("later admission callback did not progress")
	}
	unblock()
	if err := <-barrierResult; err != nil {
		t.Fatal(err)
	}
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	got := append([]int(nil), calls...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []int{1, 2}) {
		t.Fatal("blind creations lost source grouping", got)
	}
}

// Source v1.6.0 session-group-commit.test.ts:698, :770 and :796.
// Go registers the caller's continuation explicitly rather than relying on a
// Promise microtask. Grouping and publication witnesses are both public.
func TestSourceV160SessionGroupedPublicationContinuation(t *testing.T) {
	for _, mode := range []string{"visibility", "backend-read", "explicit-failure"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(bg, 5*time.Second)
			defer cancel()
			native, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			seedRootConversation(t, native)
			cause := errors.New("failed between publications")
			store := &continuationTestStore{Storage: native, entered: make(chan struct{}), release: make(chan struct{}), calls: make(chan int, 8)}
			if mode == "backend-read" {
				store.readError = cause
			}
			s, err := OpenSession(store)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(bg)
			var once sync.Once
			unblock := func() { once.Do(func() { close(store.release) }) }
			defer unblock()
			published := make(chan string, 8)
			observation, err := s.ObserveCommits(CommitObserver{Advance: func(_ context.Context, p CommitPublication) error {
				for i := range p.Changes.Len() {
					change, _ := p.Changes.Index(i)
					if immutableViewMember(t, change, "type").String() == "entry" {
						published <- immutableViewMember(t, immutableViewMember(t, change, "value"), "kind").String()
					}
				}
				return nil
			}, End: func(WatchEnd) error { return nil }})
			if err != nil {
				t.Fatal(err)
			}
			defer observation.Detach()
			turnCount := make(chan int, 1)
			results := make([]chan error, 3)
			for i, kind := range []string{"lead", "one", "two"} {
				ran := make(chan struct{})
				results[i] = make(chan error, 1)
				go func() {
					change := func(tx *Tx) (any, error) {
						entry, err := tx.AppendEntry(1, JSON{"kind": kind})
						close(ran)
						return entry, err
					}
					var err error
					if i == 1 {
						_, err = s.CommitWithContinuation(ctx, change, func(turn *PublicationTurn) error {
							turnCount <- len(published)
							switch mode {
							case "backend-read":
								_, _, err := turn.Task(999)
								if !errors.Is(err, cause) {
									t.Errorf("task read = %v; want the injected cause", err)
								}
							case "explicit-failure":
								return cause
							}
							return nil
						})
					} else {
						_, err = s.Commit(ctx, change)
					}
					results[i] <- err
				}()
				select {
				case <-ran:
				case <-ctx.Done():
					t.Fatal("callback admission", ctx.Err())
				}
				if i == 0 {
					select {
					case <-store.entered:
					case <-ctx.Done():
						t.Fatal("held first call", ctx.Err())
					}
				}
			}
			// Enter the public line after both callbacks to witness handoff of
			// their admitted parts, without waiting on either storage result.
			behind := make(chan struct{})
			go func() { _ = s.ReadOnLine(ctx, func(*ReadLine) error { close(behind); return nil }) }()
			select {
			case <-behind:
			case <-ctx.Done():
				t.Fatal("admission barrier", ctx.Err())
			}
			unblock()
			for i, done := range results {
				select {
				case err := <-done:
					if i < 2 || mode == "visibility" {
						if err != nil {
							t.Fatalf("part %d: %v", i, err)
						}
					} else {
						var fail *SessionFailed
						if !errors.As(err, &fail) || fail.Cause != cause {
							t.Fatalf("last part = %v; want SessionFailed with exact cause", err)
						}
					}
				case <-ctx.Done():
					t.Fatalf("part %d did not settle: %v", i, ctx.Err())
				}
			}
			if len(store.calls) != 2 || <-store.calls != 1 || <-store.calls != 2 {
				t.Fatal("want one held call followed by one call storing two parts")
			}
			if len(turnCount) != 1 || <-turnCount != 2 {
				t.Fatal("second continuation must see exactly two publications")
			}
			want := []string{"lead", "one"}
			if mode == "visibility" {
				want = append(want, "two")
			}
			var kinds []string
			for len(published) > 0 {
				kinds = append(kinds, <-published)
			}
			if !reflect.DeepEqual(kinds, want) {
				t.Fatalf("published kinds = %v; want %v", kinds, want)
			}
			if store.scopes.Load() != int32(len(want)) {
				t.Fatalf("requested publication scopes = %d; want %d", store.scopes.Load(), len(want))
			}
			if mode == "backend-read" && store.reads.Load() != 1 {
				t.Fatalf("storage task reads = %d; want 1", store.reads.Load())
			}
		})
	}
}

type continuationTestStore struct {
	Storage
	entered, release        chan struct{}
	calls                   chan int
	numCalls, reads, scopes atomic.Int32
	readError               error
	writeError              error
}

func (s *continuationTestStore) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	s.calls <- len(batches)
	if s.numCalls.Add(1) == 1 {
		close(s.entered)
		<-s.release
		if s.writeError != nil {
			return CommitResult{}, s.writeError
		}
	}
	return s.Storage.Commit(ctx, batches, receive)
}

func (s *continuationTestStore) Publishing(_ context.Context, publish func()) <-chan struct{} {
	s.scopes.Add(1)
	publish()
	return nil
}

func (s *continuationTestStore) Task(ctx context.Context, id ID) (TaskRecord, bool, error) {
	if id == 999 {
		s.reads.Add(1)
		return TaskRecord{}, false, s.readError
	}
	return s.Storage.Task(ctx, id)
}

// Source session-group-commit.test.ts:486: the earlier scoped Storage.task
// failure must remain the Session cause when its in-flight write later fails.
func TestSourceV160SessionColdTaskReadWinsFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 5*time.Second)
	defer cancel()
	native, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, native)
	first, later := errors.New("read failed"), errors.New("later write failed")
	store := &continuationTestStore{Storage: native, entered: make(chan struct{}), release: make(chan struct{}), calls: make(chan int, 8), readError: first, writeError: later}
	s, err := OpenSession(store)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	var once sync.Once
	unblock := func() { once.Do(func() { close(store.release) }) }
	defer unblock()
	var advances atomic.Int32
	obs, err := s.ObserveCommits(CommitObserver{Advance: func(context.Context, CommitPublication) error { advances.Add(1); return nil }, End: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer obs.Detach()
	commit := make(chan error, 1)
	go func() {
		_, err := s.Commit(ctx, func(tx *Tx) (any, error) { return tx.AppendEntry(1, JSON{"kind": "will-not-publish"}) })
		commit <- err
	}()
	select {
	case <-store.entered:
	case <-ctx.Done():
		t.Fatal("first commit never entered Storage", ctx.Err())
	}
	inside, reading := make(chan error, 1), make(chan error, 1)
	go func() {
		_, err := s.Commit(ctx, func(tx *Tx) (any, error) { _, _, err := tx.Task(999); inside <- err; return nil, err })
		reading <- err
	}()
	select {
	case err := <-inside:
		if err != first {
			t.Fatalf("cold task read = %v; want exact first cause", err)
		}
	case <-ctx.Done():
		t.Fatal("cold task read did not reach Storage during the write", ctx.Err())
	}
	if _, _, err := s.Task(ctx, 999); !errors.Is(err, first) {
		t.Fatalf("Session failure before the held write resolves = %v; want exact read cause", err)
	}
	unblock()
	select {
	case err := <-commit:
		var failed *SessionFailed
		if !errors.As(err, &failed) || failed.Cause != first {
			t.Fatalf("write = %v; want SessionFailed with exact read cause", err)
		}
	case <-ctx.Done():
		t.Fatal("write never settled", ctx.Err())
	}
	select {
	case err := <-reading:
		if err != first {
			t.Fatalf("read commit = %v; want exact read cause", err)
		}
	case <-ctx.Done():
		t.Fatal("read never settled", ctx.Err())
	}
	end, err := s.Closed(ctx)
	if err != nil || end.Reason != "failed" || end.Failure.AsError() != first || !errors.Is(end.Error, first) || advances.Load() != 0 || store.reads.Load() != 1 {
		t.Fatalf("end = %+v, %v; publications=%d reads=%d", end, err, advances.Load(), store.reads.Load())
	}
}

// Supplemental witness of transaction.ts #committedTaskRecord: repeated reads
// share one lookup inside the transaction, without inventing global negative cache.
func TestSourceV160SessionColdTaskReadMemoIsScoped(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	counted := &continuationTestStore{Storage: store}
	s, err := OpenSession(counted)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	if _, err := s.Commit(bg, func(tx *Tx) (any, error) {
		for range 2 {
			if _, found, err := tx.Task(999); found || err != nil {
				t.Fatalf("transaction read found=%t err=%v", found, err)
			}
		}
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if counted.reads.Load() != 1 {
		t.Fatalf("transaction task reads = %d; want one", counted.reads.Load())
	}
	if err := s.ReadOnLine(bg, func(read *ReadLine) error {
		for range 2 {
			if _, found, err := read.Task(999); found || err != nil {
				t.Fatalf("line read found=%t err=%v", found, err)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if counted.reads.Load() != 2 {
		t.Fatalf("transaction and line task reads = %d; want two", counted.reads.Load())
	}
	if _, found, err := s.Task(bg, 999); found || err != nil {
		t.Fatalf("session read found=%t err=%v", found, err)
	}
	if counted.reads.Load() != 3 {
		t.Fatalf("total task reads = %d; want three", counted.reads.Load())
	}
}

type coldSubmissionStorage struct {
	Storage
	witness           SubmissionRecord
	failure           error
	idReads, keyReads atomic.Int64
}

func (a *coldSubmissionStorage) Submission(ctx context.Context, id ID) (SubmissionRecord, bool, error) {
	if id != a.witness.ID {
		return a.Storage.Submission(ctx, id)
	}
	a.idReads.Add(1)
	if a.failure != nil {
		return SubmissionRecord{}, false, a.failure
	}
	return a.witness, true, nil
}

func (a *coldSubmissionStorage) SubmissionByRequest(ctx context.Context, conversation ID, key string) (SubmissionRecord, bool, error) {
	if conversation != a.witness.Conversation || key != a.witness.RequestID {
		return a.Storage.SubmissionByRequest(ctx, conversation, key)
	}
	a.keyReads.Add(1)
	if a.failure != nil {
		return SubmissionRecord{}, false, a.failure
	}
	return a.witness, true, nil
}

// Supplemental public Storage witness for session/transaction.ts committed
// submission reads. A delegated read image can lack either cold key.
func TestSourceV160SessionColdSubmissionReads(t *testing.T) {
	for _, scope := range []string{"session", "transaction"} {
		for _, by := range []string{"id", "request"} {
			for _, fault := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/fault=%t", scope, by, fault), func(t *testing.T) {
					store, err := newTestSQLite(t)
					if err != nil {
						t.Fatal(err)
					}
					seedRootConversation(t, store)
					a := &coldSubmissionStorage{Storage: store, witness: SubmissionRecord{ID: 999, Conversation: 1, RequestID: "cold", RequestIDPresent: true, Type: "input", Status: "queued"}}
					if fault {
						a.failure = errors.New("cold submission backend failure")
					}
					s, err := OpenSession(a)
					if err != nil {
						t.Fatal(err)
					}
					defer s.Close(bg)
					var record SubmissionRecord
					var found bool
					if scope == "session" {
						if by == "id" {
							record, found, err = s.Submission(bg, a.witness.ID)
						} else {
							record, found, err = s.SubmissionByRequest(bg, a.witness.Conversation, a.witness.RequestID)
						}
					} else {
						_, err = s.Commit(bg, func(tx *Tx) (any, error) {
							var readErr error
							if by == "id" {
								record, found, readErr = tx.Submission(a.witness.ID)
							} else {
								record, found, readErr = tx.SubmissionByRequest(a.witness.Conversation, a.witness.RequestID)
							}
							return nil, readErr
						})
					}
					if fault {
						if !errors.Is(err, a.failure) || found {
							t.Fatalf("cold read failure: found %t, err %v", found, err)
						}
						ctx, cancel := context.WithTimeout(bg, 3*time.Second)
						defer cancel()
						end, closeErr := s.Closed(ctx)
						if closeErr != nil || end.Failure.AsError() != a.failure || end.Reason != "failed" {
							t.Fatalf("first cause: %+v, err %v", end, closeErr)
						}
					} else if err != nil || !found || !reflect.DeepEqual(record, a.witness) {
						t.Fatalf("cold read: got %+v, found %t, err %v", record, found, err)
					}
					if by == "id" && (a.idReads.Load() != 1 || a.keyReads.Load() != 0) || by == "request" && (a.idReads.Load() != 0 || a.keyReads.Load() != 1) {
						t.Fatalf("guarded backend reads by ID %d and by request %d", a.idReads.Load(), a.keyReads.Load())
					}
				})
			}
		}
	}
}
