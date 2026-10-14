package durable

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestNestedTool120SettlementFailurePrefixAndSafeReplay(t *testing.T) {
	for _, mode := range []string{"rejected", "uncertain-before", "uncertain-after"} {
		t.Run(mode, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				var runs atomic.Int64
				var h *Harness
				var childID atomic.Uint64
				reg := wrapRegistration("settlement_fault")
				reg.ReplaySafe = true
				reg.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
					runs.Add(1)
					return ToolResult{Content: "settled"}, nil
				}
				if e := registry.Register(reg); e != nil {
					t.Fatal(e)
				}
				def := taskDefinition(t, "settlement-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					handle, e := r.StartTool(ctx, "key", "settlement_fault", JSON{})
					if e != nil {
						return e
					}
					childID.Store(uint64(handle.ID()))
					_, e = handle.Wait(ctx)
					if e != nil {
						return e
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, e := registry.RegisterTask(def); e != nil {
					t.Fatal(e)
				}
				h = openHarness(t, b.store, Options{Registry: registry})
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				core := h.session.core
				original := core.appendFrame
				cause := reject("nested settlement rejected")
				var failures atomic.Int64
				installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
					if kind == 2 {
						var commit commitRecord
						if e := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &commit); e != nil {
							return e
						}
						for _, write := range commit.Writes {
							if write.Task != nil && write.Task.Kind == nestedToolKind && taskHasDecidedOutcome(*write.Task) {
								failures.Add(1)
								if mode == "rejected" {
									return cause
								}
								if mode == "uncertain-after" && original != nil {
									if e := original(kind, ordinal, high, payload); e != nil {
										return e
									}
								}
								return errors.New("uncertain nested settlement")
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
				if e != nil || end.Reason != "failed" {
					t.Fatal(end, e)
				}
				if mode == "rejected" && !errors.Is(end.Error, cause) || mode != "rejected" && !errors.Is(end.Error, ErrPoisoned) {
					t.Fatal("failure cause", end)
				}
				if e = h.Close(ctx); e != nil {
					t.Fatal(e)
				}
				reopened := reopenStoreAfterHarnessClose(t, b.store)
				state, e := reopened.Snapshot(bg)
				if e != nil {
					t.Fatal(e)
				}
				_, journal := b.store.(*JournalStorage)
				confirmed := mode == "uncertain-after" && journal
				if taskHasDecidedOutcome(state.Tasks[ID(childID.Load())]) != confirmed || runs.Load() != 1 || failures.Load() != 1 {
					t.Fatal("confirmed settlement prefix", confirmed, runs.Load(), failures.Load())
				}
				for _, doc := range state.Documents {
					if doc.Kind == nestedCallDocument {
						var call nestedCallState
						if e = fromObject(doc.Value, &call, h.session.limits); e != nil || (call.Result != nil) != confirmed {
							t.Fatal("partial result adoption", call, e)
						}
					}
				}
				next := openHarness(t, reopened, h.options)
				record := waitPublicTask(t, next, id)
				want := int64(2)
				if confirmed {
					want = 1
				}
				if record.State.Outcome.Status != "completed" || runs.Load() != want {
					t.Fatalf("recovery:%+v err:%+v runs:%d want:%d", record.State.Outcome, record.State.Outcome.Error, runs.Load(), want)
				}
			})
		})
	}
}
