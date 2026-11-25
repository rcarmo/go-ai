package durable

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Compare candidate failure handling with the existing default, using the
// real archived worker and production RunCodemode on a SQLite task.
func TestV160CodemodeGroupedScriptFailures(t *testing.T) {
	for _, tc := range []struct {
		name, script, outputPrefix, message, frame string
	}{
		{"positionless-syntax", "const a = 1;\nconst b = ;\nreturn a", "Script error:\nSyntaxError:", "unexpected token", "codemode.js:2"},
		{"positioned-partial-output", "text(\"partial\");\nthrow new Error(\"boom\")", "==> text 1/2 <==\npartial\n==> text 2/2 <==\nScript error:", "boom", "codemode.js:2"},
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
				sandbox, remote := newJournalWorkerSandboxV160(t)
				sandbox.legacyJournal = !grouped
				registry := NewRegistry()
				type outcome struct {
					result CodemodeResult
					err    error
				}
				result := make(chan outcome, 1)
				def := taskDefinition(t, "grouped-error-v160", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					value, err := r.RunCodemode(ctx, "run", tc.script, sandbox)
					result <- outcome{value, err}
					if err != nil {
						return err
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, err := registry.RegisterTask(def); err != nil {
					t.Fatal(err)
				}
				h := openHarness(t, store, Options{Registry: registry})
				id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(ctx); err != nil {
					t.Fatal(err)
				}
				select {
				case got := <-result:
					if got.err != nil || got.result.HasValue || !strings.Contains(got.result.Error, tc.message) || !strings.HasPrefix(got.result.Output, tc.outputPrefix) || !strings.Contains(got.result.Output, tc.frame) {
						t.Fatalf("result=%+v err=%v; want preserved script failure", got.result, got.err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				if row, err := h.WaitForTask(ctx, id); err != nil || row.State.Outcome == nil || row.State.Outcome.Status != "completed" {
					t.Fatalf("task outcome=%+v err=%v; want completed", row.State.Outcome, err)
				}
				if remote.starts.Load() != 1 || remote.deliveries.Load() != 0 {
					t.Fatal("effect-free failure established a delivery")
				}
				snapshot, err := store.Snapshot(ctx)
				if err != nil {
					t.Fatal(err)
				}
				runs, journals := 0, 0
				for _, doc := range snapshot.Documents {
					if doc.Owner != id {
						continue
					}
					switch doc.Kind {
					case codemodeRunKind:
						runs++
						var state codemodeRunState
						if err := fromObject(doc.Value, &state, DefaultLimits()); err != nil {
							t.Fatal(err)
						}
						if state.Result == nil || !strings.Contains(state.Result.Error, tc.message) || grouped && !sameCodemodeTerminal(state.Terminal, codemodeTerminalPosition(0, 0, 0)) || !grouped && state.Terminal != nil {
							t.Fatalf("saved result=%+v terminal=%+v; want failed script with no effects", state.Result, state.Terminal)
						}
					case codemodeJournalKind:
						journals++
						var state codemodeJournalState
						if err := fromObject(doc.Value, &state, DefaultLimits()); err != nil {
							t.Fatal(err)
						}
						if len(state.Issued) != 0 || len(state.Deliveries) != 0 {
							t.Fatal("failure wrote a call or outcome", state)
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
