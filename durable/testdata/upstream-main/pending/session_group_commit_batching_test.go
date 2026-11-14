package durable

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

// Source f9a26365 session-group-commit.test.ts:170. The source executes three
// serial callbacks against distinct conversation-scoped document addresses:
// callbacks b/c run while a storage call is held, then b/c are stored together.
// This in-package fixture uses native append hooks and the private synchronous
// stored-observer lane because OpenSession requires native storage. The source
// holds its wrapper before delegating to underlying storage; this native hook
// holds after the core storage gate is acquired. Native ID reservations also
// use that gate, so future overlap fixes need an appropriate storage seam or
// a safe reservation strategy before this fixture can establish full source
// fidelity. It retains the admission, no-publication, call-shape, ordering and
// sequence assertions. Go goroutine completion order adapts JS promise order;
// it does not prove JS microtask/macrotask semantics.
func TestPendingSessionGroupCommitBatchesAdmittedParts(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	session, err := OpenSession(store)
	if err != nil {
		_ = store.Close(context.Background())
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	var workers sync.WaitGroup
	var releaseOnce sync.Once
	release := make(chan struct{})
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	originalFrame, originalFrames := store.appendFrame, store.appendFrames
	defer func() {
		unblock()
		cancel()
		workers.Wait()
		_ = session.Close(context.Background())
		store.appendFrame, store.appendFrames = originalFrame, originalFrames
	}()

	definition, err := DefineDocument(DefinitionOptions{
		Kind: "gc.source170.note", Version: 1, Scope: "conversation", History: "latest", Fork: "initial",
		Initial:        func(JSON) (JSON, error) { return JSON{"n": 0}, nil },
		CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { return false, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	var conversations [3]ID
	_, err = session.Commit(ctx, func(tx *Tx) error {
		for i := range conversations {
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.createConversationRecord(Conversation{ID: id}); err != nil {
				return err
			}
			conversations[i] = id
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var calls []int
	var publications []uint64
	var order []string
	hit := make(chan struct{})
	var hitOnce sync.Once
	recordCall := func(parts int) {
		mu.Lock()
		calls = append(calls, parts)
		first := len(calls) == 1
		mu.Unlock()
		if first {
			hitOnce.Do(func() { close(hit) })
			<-release
		}
	}
	store.appendFrame = func(kind byte, ordinal, high uint64, payload []byte) error {
		if kind == 2 {
			recordCall(1)
		}
		return originalFrame(kind, ordinal, high, payload)
	}
	store.appendFrames = func(frames []storageFrame) error {
		parts := 0
		for _, frame := range frames {
			if frame.kind == 2 {
				parts++
			}
		}
		if parts > 0 {
			recordCall(parts)
		}
		return originalFrames(frames)
	}
	_, err = session.observeStoredCommits(sessionCommitObserver{
		advance: func(_ context.Context, publication CommitPublication) error {
			mu.Lock()
			publications = append(publications, publication.Seq)
			mu.Unlock()
			return nil
		},
		end: func(WatchEnd) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}

	type result struct {
		name string
		seq  uint64
		err  error
	}
	results := make(chan result, 3)
	ran := make(chan string, 2)
	start := func(name string, id ID, admission *source170Admission) {
		workers.Add(1)
		go func() {
			defer workers.Done()
			jobCtx := context.Context(ctx)
			if admission != nil {
				jobCtx = admission
			}
			seq, err := session.Commit(jobCtx, func(tx *Tx) error {
				doc, err := tx.AcquireDocument(definition, id, nil, nil)
				if err != nil {
					return err
				}
				if err := doc.Update(func(value JSON) error { value["n"] = 1; return nil }); err != nil {
					return err
				}
				if name != "a" {
					ran <- name
				}
				return nil
			})
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			results <- result{name: name, seq: seq, err: err}
		}()
	}
	start("a", conversations[0], nil)
	select {
	case <-hit:
	case <-ctx.Done():
		t.Fatal("first commit did not reach the held storage call")
	}
	for i, name := range []string{"b", "c"} {
		admission := &source170Admission{Context: ctx, attempted: make(chan struct{})}
		start(name, conversations[i+1], admission)
		select {
		case <-admission.attempted:
		case <-ctx.Done():
			t.Fatalf("%s did not attempt Session admission", name)
		}
	}
	// The admission witnesses ensure b/c attempted the line, rather than only
	// proving their goroutines were created. Source waits for both callbacks.
	var during []string
	deadline := time.NewTimer(time.Second)
waitCallbacks:
	for len(during) < 2 {
		select {
		case name := <-ran:
			during = append(during, name)
		case <-deadline.C:
			break waitCallbacks
		case <-ctx.Done():
			break waitCallbacks
		}
	}
	deadline.Stop()
	seen := map[string]bool{}
	for _, name := range during {
		seen[name] = true
	}
	if len(during) != 2 || !seen["b"] || !seen["c"] {
		t.Errorf("source170 callbacks before release: got %v, want both b and c", during)
	}
	mu.Lock()
	before := append([]uint64(nil), publications...)
	mu.Unlock()
	if len(before) != 0 {
		t.Errorf("source170 publications before release: %v", before)
	}
	unblock()
	for range 3 {
		select {
		case got := <-results:
			if got.err != nil {
				t.Errorf("commit %s: %v", got.name, got.err)
			}
		case <-ctx.Done():
			t.Fatal("commits did not settle after release")
		}
	}
	workers.Wait()
	mu.Lock()
	gotCalls := append([]int(nil), calls...)
	gotOrder := append([]string(nil), order...)
	gotSeqs := append([]uint64(nil), publications...)
	mu.Unlock()
	if !reflect.DeepEqual(gotCalls, []int{1, 2}) {
		t.Errorf("source170 storage call sizes: got %v, want [1 2]", gotCalls)
	}
	if !reflect.DeepEqual(gotOrder, []string{"a", "b", "c"}) {
		t.Errorf("source170 settlement order: got %v, want [a b c]", gotOrder)
	}
	if len(gotSeqs) != 3 || !(gotSeqs[0] < gotSeqs[1] && gotSeqs[1] < gotSeqs[2]) {
		t.Errorf("source170 distinct ordered publication sequences: %v", gotSeqs)
	}
}

type source170Admission struct {
	context.Context
	once      sync.Once
	attempted chan struct{}
}

func (c *source170Admission) Done() <-chan struct{} {
	c.once.Do(func() { close(c.attempted) })
	return c.Context.Done()
}
