package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120RejectedDrainCommitReopensWithoutDuplicateEffects(t *testing.T) {
	for _, initial := range []bool{true, false} {
		t.Run(map[bool]string{true: "initial", false: "final"}[initial], func(t *testing.T) { testRejectedDrainCommit120(t, initial, false) })
	}
}
func TestCodemode120ParallelRejectedDrainCommitReopen(t *testing.T) {
	for _, initial := range []bool{true, false} {
		t.Run(map[bool]string{true: "initial", false: "final"}[initial], func(t *testing.T) { testRejectedDrainCommit120(t, initial, true) })
	}
}
func testRejectedDrainCommit120(t *testing.T, initial, parallel bool) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var effects atomic.Int64
		reg := wrapRegistration("codemode_leaf")
		reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "leaf"}, nil
		}
		if e := registry.Register(reg); e != nil {
			t.Fatal(e)
		}
		sandbox, err := NewQuickJSSandbox(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer sandbox.Close(bg)
		sandbox.AcknowledgeDrains = true
		sandbox.ParallelCalls = parallel
		rejectCount := func(journal codemodeJournalState) bool {
			if initial {
				return journal.FirstIssued != nil
			}
			return len(journal.Deliveries) > 0 && journal.Deliveries[0].IssuedAfter != nil
		}
		def := taskDefinition(t, "journal-fault-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			_, e := r.RunCodemode(ctx, "run", `await tools.codemode_leaf({});store("confirmed",true);return "done";`, sandbox)
			if e != nil {
				return e
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		core := h.session.core
		original := core.appendFrame
		cause := reject("codemode drain count rejected")
		var failures atomic.Int64
		installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
			if kind == 2 {
				var record commitRecord
				if e := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); e != nil {
					return e
				}
				for _, write := range record.Writes {
					if write.Delta != nil {
						doc := core.state.Documents[write.Delta.ID]
						if doc.Kind == codemodeJournalKind {
							value, e := ApplyOperations(doc.Value, write.Delta.Ops, h.session.limits)
							if e != nil {
								return e
							}
							var journal codemodeJournalState
							if e = fromObject(JSON(value.(map[string]any)), &journal, h.session.limits); e != nil {
								return e
							}
							if rejectCount(journal) {
								failures.Add(1)
								return cause
							}
						}
					}
					if write.Document != nil && write.Document.Kind == codemodeJournalKind {
						var journal codemodeJournalState
						if e := fromObject(write.Document.Value, &journal, h.session.limits); e != nil {
							return e
						}
						if rejectCount(journal) {
							failures.Add(1)
							return cause
						}
					}
				}
			}
			if original != nil {
				return original(kind, ordinal, high, payload)
			}
			return nil
		})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		end, e := h.session.Closed(ctx)
		if e != nil || end.Reason != "failed" || !errors.Is(end.Error, cause) {
			t.Fatal(end, e)
		}
		if e = h.Close(ctx); e != nil {
			t.Fatal(e)
		}
		reopened := reopenStoreAfterHarnessClose(t, b.store)
		state, e := reopened.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		for _, doc := range state.Documents {
			if doc.Kind == codemodeJournalKind {
				var journal codemodeJournalState
				wantIssued := 1
				if parallel && initial {
					wantIssued = 0
				}
				if e = fromObject(doc.Value, &journal, h.session.limits); e != nil || len(journal.Issued) != wantIssued {
					t.Fatal("rejected drain prefix", journal, e)
				}
				if initial {
					if journal.FirstIssued != nil || len(journal.Deliveries) != 0 || effects.Load() != 0 {
						t.Fatal("initial rejection admitted effect", journal, effects.Load())
					}
				} else {
					if journal.FirstIssued == nil || len(journal.Deliveries) != 1 || journal.Deliveries[0].IssuedAfter != nil || effects.Load() != 1 {
						t.Fatal("final rejection adopted count", journal, effects.Load())
					}
				}
			}
		}
		next := openHarness(t, reopened, h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || effects.Load() != 1 || failures.Load() != 1 {
			t.Fatalf("%+v %+v effects:%d writes:%d", record.State.Outcome, record.State.Outcome.Error, effects.Load(), failures.Load())
		}
	})
}
