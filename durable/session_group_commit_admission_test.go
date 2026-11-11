package durable

import (
	"context"
	"errors"
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
			var commitChange func(context.Context, func(*Tx) error) (uint64, error)
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
			if _, err = commitChange(bg, func(tx *Tx) error {
				for i := 0; i < 2; i++ {
					id, e := tx.MintID()
					if e != nil {
						return e
					}
					if e := tx.CreateConversation(Conversation{ID: id}); e != nil {
						return e
					}
					owners = append(owners, id)
				}
				return nil
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
			observation, err := s.observeStoredCommits(sessionCommitObserver{advance: func(_ context.Context, p CommitPublication) error {
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
					_, e := commitChange(bg, func(tx *Tx) error {
						doc, e := tx.AcquireDocument(def, owners[i], nil, nil)
						if e != nil {
							return e
						}
						if e = doc.Set(JSON{"n": 1}); e != nil {
							return e
						}
						ran <- keys[i]
						return nil
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
			_, err := s.Commit(bg, func(tx *Tx) error {
				content := EntryContent{Value: JSON{"index": i}}
				if i > 0 {
					content.Head = firstID
				}
				entry, err := tx.AppendTypedEntry(def, 1, content)
				if i == 0 {
					firstID = entry.ID
				}
				close(ran)
				return err
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
			if _, err := s.Commit(bg, func(tx *Tx) error {
				if _, err := tx.AcquireDocument(a, 1, nil, nil); err != nil {
					return err
				}
				if _, err := tx.AcquireDocument(b, 1, nil, nil); err != nil {
					return err
				}
				var err error
				at, err = tx.MintID()
				if err != nil {
					return err
				}
				return tx.AppendEntry(Entry{ID: at, Conversation: 1, Kind: "cutoff", Value: JSON{}})
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
				_, err := s.Commit(bg, func(tx *Tx) error {
					doc, err := tx.AcquireDocument(a, 1, nil, nil)
					if err != nil {
						return err
					}
					return doc.Set(JSON{"n": 1})
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

// Consolidated from session_read_line_v150_test.go.

// Pinned f9 session-group-commit.test.ts:256. These are Session-line reads,
// not direct raw Storage calls (which have no Session job-order guarantee).
func TestV150SessionReadLinePendingConversation(t *testing.T) {
	for _, name := range []string{"latestEntryId", "latestHeadMarker", "tx.scanEntries", "line.scanEntries", "line.findLatestHeadMarker"} {
		t.Run(name, func(t *testing.T) {
			session, _, held, ctx := openGatesSession(t)
			held.holdCommits()
			created := make(chan ID, 1)
			settled := make(chan error, 1)
			runGatesJob(t, session, ctx, func(tx *Tx) error {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				if err := tx.CreateConversation(Conversation{ID: id}); err != nil {
					return err
				}
				created <- id
				return nil
			}, settled)
			var id ID
			select {
			case id = <-created:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case <-held.entered:
			case <-ctx.Done():
				t.Fatal("creation never reached storage")
			}
			reading := make(chan error, 1)
			started := make(chan struct{})
			go func() {
				close(started)
				var err error
				switch name {
				case "latestEntryId":
					_, err = session.Commit(ctx, func(tx *Tx) error {
						value, found, e := tx.LatestEntryID(id)
						if e == nil && (found || value != 0) {
							return errors.New("unexpected latest entry")
						}
						return e
					})
				case "latestHeadMarker":
					_, err = session.Commit(ctx, func(tx *Tx) error {
						_, found, e := tx.LatestHeadMarker(id, 0)
						if e == nil && found {
							return errors.New("unexpected head marker")
						}
						return e
					})
				case "tx.scanEntries":
					_, err = session.Commit(ctx, func(tx *Tx) error {
						page, e := tx.ScanEntries(EntryQuery{Conversation: id}, 1, nil)
						if e == nil && len(page.Items) != 0 {
							return errors.New("unexpected entries")
						}
						return e
					})
				case "line.scanEntries":
					err = session.ReadOnLine(ctx, func(line *ReadLine) error {
						page, e := line.ScanEntries(EntryQuery{Conversation: id}, 1, nil)
						if e == nil && len(page.Items) != 0 {
							return errors.New("unexpected entries")
						}
						return e
					})
				case "line.findLatestHeadMarker":
					err = session.ReadOnLine(ctx, func(line *ReadLine) error {
						_, found, e := line.FindLatestHeadMarker(id, 0)
						if e == nil && found {
							return errors.New("unexpected marker")
						}
						return e
					})
				}
				reading <- err
			}()
			<-started
			select {
			case err := <-reading:
				t.Fatalf("read settled before publication: %v", err)
			case <-time.After(30 * time.Millisecond):
			}
			held.releaseHold()
			if err := settleWithinGate(t, settled, 3); err != nil {
				t.Fatal(err)
			}
			if err := settleWithinGate(t, reading, 3); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestV150SessionReadLineLifetimeAndCancellation(t *testing.T) {
	session, _, _, ctx := openGatesSession(t)
	var escaped *ReadLine
	if err := session.ReadOnLine(ctx, func(line *ReadLine) error { escaped = line; _, _, err := line.FindLatestHeadMarker(1, 0); return err }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := escaped.FindLatestHeadMarker(1, 0); !errors.Is(err, ErrSealed) {
		t.Fatalf("escaped read=%v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	called := false
	if err := session.ReadOnLine(canceled, func(*ReadLine) error { called = true; return nil }); !errors.Is(err, context.Canceled) || called {
		t.Fatalf("canceled read called=%v error=%v", called, err)
	}
}

// Source session-group-commit:759, plus single-part scope/once/deferred controls
// derived from session.ts #publishing. Callback result is captured in Go rather
// than the source's generic Promise result. Storage and publication are real.
type v150PublicationScopeStorage struct {
	Storage
	core  *storeCore
	scope func(context.Context, func())
}

func (w *v150PublicationScopeStorage) nativeCore() *storeCore { return w.core }
func (w *v150PublicationScopeStorage) Publishing(ctx context.Context, publish func()) {
	w.scope(ctx, publish)
}
func TestSessionV150StoragePublicationScope(t *testing.T) {
	for _, mode := range []string{"inside", "twice", "reentrant", "throw-before", "throw-after", "deferred"} {
		t.Run(mode, func(t *testing.T) {
			s := gatedSession(t)
			def, err := DefineDocument(DefinitionOptions{Kind: "scope.counter", Scope: "session", Version: 1, Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil }})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Commit(bg, func(tx *Tx) error { _, err := tx.AcquireDocument(def, 0, nil, nil); return err }); err != nil {
				t.Fatal(err)
			}
			marker := errors.New("no scope")
			var reports []error
			s.timerReport = func(err error) { reports = append(reports, err) }
			depth, calls, published := 0, 0, 0
			var saved func()
			observer, err := s.observeStoredCommits(sessionCommitObserver{advance: func(context.Context, CommitPublication) error {
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
				_, err := s.Commit(context.WithValue(bg, v150ScopeContextKey{}, "producer"), func(tx *Tx) error {
					doc, err := tx.AcquireDocument(def, 0, nil, nil)
					if err != nil {
						return err
					}
					value, err := doc.Get()
					if err != nil {
						return err
					}
					prior = value["n"]
					return doc.ApplyOperations([]Operation{{"s", []any{"n"}, 1}})
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
	observer, err := s.observeStoredCommits(sessionCommitObserver{advance: func(_ context.Context, p CommitPublication) error {
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
		_, err := s.Commit(bg, func(tx *Tx) error {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: kind, Value: JSON{}})
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
