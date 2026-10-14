package durable

import (
	"context"
	"testing"
)

type storeSandbox120 struct {
	value  any
	fail   bool
	seen   JSON
	starts int
}

func (s *storeSandbox120) Start(_ context.Context, _ string, start CodemodeStart) (CodemodeExecution, error) {
	s.starts++
	s.seen = start.Store
	if s.seen != nil {
		s.seen["sandboxOnly"] = "not durable"
	}
	return &storeExecution120{s}, nil
}

type storeExecution120 struct{ sandbox *storeSandbox120 }

func (e *storeExecution120) Next(context.Context) (CodemodeEvent, error) {
	result := &CodemodeResult{Output: "store", StoreWrites: JSON{"key": e.sandbox.value}}
	if e.sandbox.fail {
		result.Error = "script failed"
	}
	return CodemodeEvent{Result: result}, nil
}
func (e *storeExecution120) Deliver(context.Context, CodemodeDelivery) error { return nil }
func (e *storeExecution120) Close() error                                    { return nil }
func TestCodemode120StoreSnapshotSuccessfulOnlyAndIdempotent(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		def := taskDefinition(t, "store-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			first := &storeSandbox120{value: JSON{"n": 1}}
			if _, e := r.RunCodemode(ctx, "one", "source", first); e != nil {
				return e
			}
			if _, e := r.RunCodemode(ctx, "one", "source", first); e != nil {
				return e
			}
			if first.starts != 1 {
				t.Fatal("settled store replayed")
			}
			second := &storeSandbox120{value: JSON{"n": 2}, fail: true}
			if _, e := r.RunCodemode(ctx, "two", "source2", second); e != nil {
				return e
			}
			seen, ok := second.seen["key"].(map[string]any)
			if !ok || !equalJSONValue(seen["n"], 1) {
				t.Fatal("store snapshot missing", second.seen)
			}
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				store, e := tx.AcquireDocument(codemodeStoreDefinition(), 1, nil, nil)
				if e != nil {
					return nil, e
				}
				value, e := store.Get()
				if e != nil {
					return nil, e
				}
				values := value["values"].(map[string]any)
				if !equalJSONValue(values["key"].(map[string]any)["n"], 1) || values["sandboxOnly"] != nil {
					t.Fatal("failed writes or VM alias persisted", value)
				}
				return taskDone(nil), nil
			})
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
		state, e := h.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, doc := range state.Documents {
			if doc.Kind == "pi.codemode.store" {
				found = true
				if doc.Retired || doc.History != "rewindable" || doc.Fork != "asOf" {
					t.Fatal(doc)
				}
			}
		}
		if !found {
			t.Fatal("conversation store missing")
		}
		var entry ID
		_, e = h.CommitTasks(bg, 1, func(tx *Tx) error {
			var e error
			entry, e = tx.MintID()
			if e != nil {
				return e
			}
			return tx.AppendEntry(Entry{ID: entry, Conversation: 1, Kind: "app", Value: JSON{"text": "fork point"}})
		})
		if e != nil {
			t.Fatal(e)
		}
		conv := &ConversationHandle{h: h, id: 1}
		fork, e := conv.Fork(bg, entry)
		if e != nil {
			t.Fatal(e)
		}
		forkState, e := h.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		forkFound := false
		for _, doc := range forkState.Documents {
			if doc.Kind == "pi.codemode.store" && doc.Owner == fork.ID() {
				forkFound = true
				if !equalJSONValue(doc.Value["values"].(map[string]any)["key"].(map[string]any)["n"], 1) {
					t.Fatal("fork store lost snapshot", doc)
				}
			}
		}
		if !forkFound {
			t.Fatal("fork store missing")
		}
		if e = h.Close(bg); e != nil {
			t.Fatal(e)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		reopened, e := next.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		for _, doc := range reopened.Documents {
			if doc.Kind == "pi.codemode.store" && len(doc.Value["values"].(map[string]any)) != 1 {
				t.Fatal("reopen changed store", doc.Value)
			}
		}
	})
}
