package durable

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type failNextJournalCommitV160 struct {
	Storage
	armed   atomic.Bool
	cause   error
	attempt chan []byte
}

func (s *failNextJournalCommitV160) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	if !s.armed.CompareAndSwap(true, false) {
		return s.Storage.Commit(ctx, batches, receive)
	}
	wire, err := json.Marshal(batches)
	if err != nil {
		return CommitResult{}, err
	}
	s.attempt <- wire
	return CommitResult{}, s.cause
}

// codemode.test.ts:791–820, through the published portable guest. The public
// storage decorator refuses the commit armed by first's callback. Wait for
// the failed harness to close and join instead of using the source's timer.
func TestSourceV160CodemodePublishedWorkerResultAfterJournal(t *testing.T) {
	sandbox, remote := newJournalWorkerSandboxV160(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	failure := &failNextJournalCommitV160{Storage: store, cause: errors.New("lost"), attempt: make(chan []byte, 1)}
	registry := NewRegistry()
	ext, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&ext); err != nil {
		t.Fatal(err)
	}
	var seen []string
	var mu sync.Mutex
	mark := func(name string) {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, name)
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("s1", "codemode", JSON{"code": `await first(); await second();`})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	options.CodemodeGlobals = []CodemodeGlobal{
		{Name: "first", Version: 1, Execute: func(context.Context, any) (any, error) {
			mark("first")
			failure.armed.Store(true)
			return 1, nil
		}},
		{Name: "second", Version: 1, Execute: func(context.Context, any) (any, error) {
			mark("second")
			return 2, nil
		}},
	}
	h := openHarness(t, failure, options)
	conversation := root(t, h, ref)
	if _, err := conversation.Submit(ctx, Input{Content: "go"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	end, err := h.Closed(ctx)
	if err != nil || end.Reason != "failed" || !errors.Is(end.Error, failure.cause) {
		t.Fatalf("harness close = %+v, %v; want lost commit", end, err)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	got := append([]string(nil), seen...)
	mu.Unlock()
	if !reflect.DeepEqual(got, []string{"first"}) {
		t.Errorf("callbacks = %q; want [first]", got)
	}
	// Supplemental: even if a premature reply loses the race to shutdown,
	// it must never cross the VM delivery boundary in the first place.
	if delivered := remote.deliveries.Load(); delivered != 0 {
		t.Errorf("published worker received %d deliveries after refusing the commit; want 0", delivered)
	}
	if remote.starts.Load() != 1 || requests.Load() != 1 {
		t.Errorf("guest starts = %d, provider requests = %d; want 1 each", remote.starts.Load(), requests.Load())
	}
	var batches [][]StorageWrite
	select {
	case wire := <-failure.attempt:
		if err := json.Unmarshal(wire, &batches); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatal("failed storage commit not observed")
	}
	checkV160RefusedJournalPrefix(t, ctx, store, batches)
}

// Verify the failed public batch really was the first global's delivery and
// that reopening SQLite does not recover any part of that refused commit.
func checkV160RefusedJournalPrefix(t *testing.T, ctx context.Context, store Storage, batches [][]StorageWrite) {
	t.Helper()
	reopened := reopenStoreAfterHarnessClose(t, store)
	defer reopened.Close(context.Background())
	state, err := readStorageProjection(ctx, reopened, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	matches := 0
	for id, doc := range state.Documents {
		if doc.Kind != codemodeJournalKind {
			continue
		}
		var persisted codemodeJournalState
		if err := decodeDocumentValue(doc, DefaultLimits(), &persisted); err != nil {
			t.Fatal(err)
		}
		if len(persisted.Issued) != 1 || persisted.Issued[0].Name != "first" || len(persisted.Deliveries) != 0 {
			t.Fatalf("reopened journal = %+v; want first issued and no delivery", persisted)
		}
		for _, batch := range batches {
			for _, write := range batch {
				if write.Type != "document.change" || write.ID != id || write.Content == nil {
					continue
				}
				matches++
				value := write.Content.Value
				if write.Content.Kind == "delta" {
					next, err := ApplyOperations(doc.Value, write.Content.Ops, DefaultLimits())
					if err != nil {
						t.Fatal(err)
					}
					object, ok := next.(map[string]any)
					if !ok {
						t.Fatalf("refused journal update result = %T; want object", next)
					}
					value = object
				}
				var refused codemodeJournalState
				if err := fromObject(value, &refused, DefaultLimits()); err != nil {
					t.Fatal(err)
				}
				if len(refused.Deliveries) != 1 || refused.Deliveries[0].Call.Name != "first" || !refused.Deliveries[0].HasValue || !equalJSONValue(refused.Deliveries[0].Value, 1) {
					t.Fatalf("refused journal candidate = %+v; want first's delivery of 1", refused)
				}
			}
		}
	}
	if matches != 1 {
		t.Fatalf("refused journal changes = %d; want 1", matches)
	}
}
