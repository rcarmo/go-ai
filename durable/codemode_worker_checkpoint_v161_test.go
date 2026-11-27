package durable

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

type codemodeCheckpointStoreV161 struct {
	Storage
	mu   sync.Mutex
	last []StorageWrite
}

func (s *codemodeCheckpointStoreV161) Commit(ctx context.Context, batches [][]StorageWrite, receive func(int, uint64)) (CommitResult, error) {
	return s.Storage.Commit(ctx, batches, func(i int, seq uint64) {
		s.mu.Lock()
		s.last = append([]StorageWrite(nil), batches[i]...)
		s.mu.Unlock()
		if receive != nil {
			receive(i, seq)
		}
	})
}

// Source: pi v1.6.1 packages/durable/test/codemode-checkpoints.test.ts:14–59.
func TestSourceV161CodemodeStoreCheckpointAndFork(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	sandbox, remote := newJournalWorkerSandbox(officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"))
	registry := NewRegistry()
	ext, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&ext); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		out := make(chan goai.Event, 1)
		if requests.Add(1)%2 == 1 {
			out <- toolAnswer("checkpoint-read", "codemode", JSON{"code": `return load("counter");`})
		} else {
			out <- terminal("done")
		}
		close(out)
		return out
	})
	options.Registry = registry
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &codemodeCheckpointStoreV161{Storage: store}
	h := openHarness(t, recorder, options)
	parent := root(t, h, ref)
	owner := parent.ID()
	definition := codemodeStoreDefinition()
	counterValue := func(counter int) JSON { return JSON{"values": JSON{"counter": counter}} }
	write := func(counter int, withEntry bool) ID {
		t.Helper()
		value, err := parent.Commit(ctx, func(tx *Tx) (any, error) {
			doc, err := tx.AcquireDocument(definition, owner, nil, nil)
			if err != nil {
				return nil, err
			}
			if err := doc.ApplyOperations([]Operation{{"s", []any{"values", "counter"}, counter}}); err != nil {
				return nil, err
			}
			if !withEntry {
				return ID(0), nil
			}
			entry, err := tx.AppendEntry(owner, EntryDraft{"kind": "test.counter"})
			return entry.ID, err
		})
		if err != nil {
			t.Fatalf("counter %d: %v", counter, err)
		}
		return value.(ID)
	}
	cutoff := write(0, true)
	record, found, err := store.FindDocument(ctx, DocumentAddress{Kind: "pi.codemode.store", Scope: "conversation", Owner: owner}, CurrentDocumentPoint())
	if err != nil || !found {
		t.Fatalf("current store record missing: %v (%t)", err, found)
	}
	for counter := 1; counter <= 64; counter++ {
		entry := write(counter, true)
		if counter == 32 {
			cutoff = entry
		}
	}
	assertDeltaCount := func(want uint64) {
		t.Helper()
		current, found, err := store.DocumentAt(ctx, record.ID, CurrentDocumentPoint())
		if err != nil || !found || current.DeltasSinceBase != want {
			t.Fatalf("deltas since base = %d, found=%v err=%v; want %d", current.DeltasSinceBase, found, err, want)
		}
	}
	assertDeltaCount(64)
	write(65, false)
	assertDeltaCount(0)
	recorder.mu.Lock()
	last := append([]StorageWrite(nil), recorder.last...)
	recorder.mu.Unlock()
	if len(last) != 1 || last[0].Type != "document.change" || last[0].ID != record.ID || last[0].Content == nil ||
		last[0].Content.Kind != "base" || last[0].Content.Version != 1 || len(last[0].Content.Ops) != 0 || !equalJSONValue(last[0].Content.Value, counterValue(65)) {
		t.Fatalf("checkpoint receipt = %+v; want one base write of counter 65", last)
	}
	past, found, err := parent.SnapshotDefinitionAsOf(ctx, definition, nil, cutoff)
	if err != nil || !found || !equalJSONValue(past.Copy(), counterValue(32)) {
		t.Fatalf("historical snapshot = %v, found=%v, err=%v; want counter 32", past, found, err)
	}
	fork, err := parent.Fork(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	forkStore, found, err := h.SnapshotDefinition(ctx, definition, fork.ID(), nil)
	if err != nil || !found {
		t.Fatalf("fork snapshot missing: found=%v, err=%v", found, err)
	}
	if got := forkStore.Copy(); !equalJSONValue(got, counterValue(32)) {
		t.Fatalf("fork snapshot = %v; want counter 32", got)
	}
	write(66, false)
	assertDeltaCount(1)

	// Supplemental: close and reopen SQLite, then read the values in the
	// published guest. The publisher did not run the 65 fixture writes.
	forkID := fork.ID()
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	h = openHarness(t, reopenStoreAfterHarnessClose(t, store), options)
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		id   ID
		body string
	}{{owner, "66"}, {forkID, "32"}} {
		conversation, err := h.Conversation(ctx, tc.id)
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conversation.Submit(ctx, Input{Content: "read the checkpoint"})
		if err != nil {
			t.Fatal(err)
		}
		if settled, err := sub.Wait(ctx); err != nil || settled.Submission.Status != "done" {
			t.Fatalf("guest read did not settle: %+v, %v", settled, err)
		}
		result := v160ToolResult(t, ctx, conversation, "checkpoint-read")
		_, body, ok := strings.Cut(v160ToolResultText(result), "\nOutput:\n")
		if result.IsError || !ok || body != tc.body {
			t.Fatalf("guest store read for %d = %q, error=%v; want %q", tc.id, v160ToolResultText(result), result.IsError, tc.body)
		}
	}
	if remote.starts.Load() != 2 || remote.deliveries.Load() != 0 || requests.Load() != 4 {
		t.Fatalf("starts/deliveries/model requests = %d/%d/%d; want 2/0/4", remote.starts.Load(), remote.deliveries.Load(), requests.Load())
	}
}
