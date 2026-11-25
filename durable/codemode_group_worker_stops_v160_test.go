package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

type failedWorkerLaunchV160 struct {
	process officialWorkerTransport120
	frames  *atomic.Int32
}

func (f failedWorkerLaunchV160) Execute(ctx context.Context, request CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
	return f.process.executeProcess(ctx, "worker-no-wasm-v160.mjs", request, func(ctx context.Context, batch []CodemodeWorkerMessage) (*CodemodeWorkerReply, error) {
		if len(batch) != 1 || batch[0].Type != "failure" || batch[0].During != nil {
			return nil, fmt.Errorf("published launcher sent unexpected frames: %+v", batch)
		}
		var detail struct{ Kind, Message string }
		if err := json.Unmarshal(batch[0].Error, &detail); err != nil || detail.Kind != "sandbox" || detail.Message != "Failed to load QuickJS: no wasm" {
			return nil, fmt.Errorf("published launch failure = %+v; %v", detail, err)
		}
		f.frames.Add(1)
		return exchange(ctx, batch)
	})
}

type stoppedAfterReplyV160 struct {
	base   CodemodeWorkerTransport
	frames *atomic.Int32
}

func (f stoppedAfterReplyV160) Execute(ctx context.Context, request CodemodeWorkerRequest, exchange func(context.Context, []CodemodeWorkerMessage) (*CodemodeWorkerReply, error)) error {
	return f.base.Execute(ctx, request, func(ctx context.Context, batch []CodemodeWorkerMessage) (*CodemodeWorkerReply, error) {
		for i, m := range batch {
			if m.Type == "done" {
				if !m.OK || m.During == nil || m.During.Seq != 1 || m.During.Issued != 1 {
					return nil, fmt.Errorf("expected one real result before simulated stop: %+v", m)
				}
				owned := append([]CodemodeWorkerMessage(nil), batch...)
				owned[i] = CodemodeWorkerMessage{Type: "crash", Message: "worker stopped after reply"}
				f.frames.Add(1)
				return exchange(ctx, owned)
			}
		}
		return exchange(ctx, batch)
	})
}

// The launch case uses the actual published synthesis of the sandbox failure
// at sandbox.test.ts:841–848. The other case injects a crash frame after a real
// published guest has received one callback reply. New mode must not pretend
// the unpositioned crash included a guest delivery watermark.
func TestV160CodemodeGroupedWorkerStops(t *testing.T) {
	for _, tc := range []struct {
		name, script, message string
		calls                 int32
	}{
		{"launch", `return 1`, "Failed to load QuickJS: no wasm", 0},
		{"after-reply", `return await receipt("a")`, "worker stopped after reply", 1},
	} {
		for _, grouped := range []bool{false, true} {
			mode := "legacy"
			if grouped {
				mode = "grouped"
			}
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
				defer cancel()
				store, err := newTestSQLite(t)
				if err != nil {
					t.Fatal(err)
				}
				sandbox, worker := newJournalWorkerSandboxV160(t)
				sandbox.legacyJournal = !grouped
				var calls, frames atomic.Int32
				if tc.calls == 0 {
					worker.CodemodeWorkerRemote.Transport = failedWorkerLaunchV160{process: officialWorkerTransport120{root: officialWorkerPackageRootV160(t)}, frames: &frames}
				} else {
					worker.CodemodeWorkerRemote.Transport = stoppedAfterReplyV160{base: worker.CodemodeWorkerRemote.Transport, frames: &frames}
				}
				registry := NewRegistry()
				definition := taskDefinition(t, "worker-stop-v160", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					for i := 0; i < 2; i++ {
						result, err := r.RunCodemode(ctx, "run", tc.script, sandbox)
						if err != nil || result.HasValue || result.Error != tc.message || result.Output != "" {
							return fmt.Errorf("read %d: result=%+v err=%v; want the preserved worker stop %q", i+1, result, err, tc.message)
						}
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, err := registry.RegisterTask(definition); err != nil {
					t.Fatal(err)
				}
				globals := []CodemodeGlobal{{Name: "receipt", Version: 1, Execute: func(_ context.Context, value any) (any, error) {
					if value != "a" {
						return nil, fmt.Errorf("unexpected callback value %v", value)
					}
					calls.Add(1)
					return "a", nil
				}}}
				h := openHarness(t, store, Options{Registry: registry, CodemodeGlobals: globals})
				id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(ctx); err != nil {
					t.Fatal(err)
				}
				row, err := h.WaitForTask(ctx, id)
				if err != nil || row.State.Outcome == nil || row.State.Outcome.Status != "completed" {
					if row.State.Outcome != nil && row.State.Outcome.Error != nil {
						t.Fatal(row.State.Outcome.Error.Message)
					}
					t.Fatalf("worker-stop task outcome = %+v; %v", row.State.Outcome, err)
				}
				if frames.Load() != 1 || calls.Load() != tc.calls || worker.starts.Load() != 1 || worker.deliveries.Load() != tc.calls {
					t.Fatalf("frames=%d calls=%d starts=%d deliveries=%d", frames.Load(), calls.Load(), worker.starts.Load(), worker.deliveries.Load())
				}
				snapshot, err := store.Snapshot(ctx)
				if err != nil {
					t.Fatal(err)
				}
				var runs, journals int
				for _, doc := range snapshot.Documents {
					if doc.Owner != id {
						continue
					}
					switch doc.Kind {
					case codemodeRunKind:
						runs++
						want := JSON{"workerStopped": true, "issued": int(tc.calls), "committed": int(tc.calls)}
						if grouped && !equalJSONValue(doc.Value["terminal"], want) || !grouped && doc.Value["terminal"] != nil {
							t.Fatalf("worker stop terminal=%v; no guest delivery position is available", doc.Value["terminal"])
						}
					case codemodeJournalKind:
						journals++
						var journal codemodeJournalState
						if err := fromObject(doc.Value, &journal, DefaultLimits()); err != nil {
							t.Fatal(err)
						}
						if len(journal.Issued) != int(tc.calls) || len(journal.Deliveries) != int(tc.calls) {
							t.Fatal("worker stop changed the committed outcomes", journal)
						}
					}
				}
				if runs != 1 || journals != 1 {
					t.Fatalf("run and journal count %d/%d; want 1/1", runs, journals)
				}
			})
		}
	}
}
