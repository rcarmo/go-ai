package durable

import (
	"context"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Actual pinned source case harness-after-run.test.ts923 (#11141): "gives a run without a
// recorded first entry its answer as first". Rebuilt through the public transaction surface
// only: the hook's TaskRuntime.Commit and Tx.Document handle Update (the write path that
// actually stages a change - reading the handle with Get and mutating the returned value is
// a fixture artifact, not a contract check). The recorded first entry is removed during the
// run, and the answer summary must report it as the answer.

// Actual pinned source case harness-after-run.test.ts line 923 (#11141):
// "gives a run without a recorded first entry its answer as first".
// The source's onYield hook mutates the live document inside a commit
// (delete run.first), then the afterRun hook must see first == answer.
// Rebuilt through the public transaction surface: TaskRuntime.Commit + Tx.Document
// handle Update. No API expansion.
func TestMainAfterRunRunWithoutRecordedFirstEntryAnswersAsFirst(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var h *Harness
		registry := NewRegistry()
		var seen RunSummary
		mutated := false
		var before any
		if err := registry.Install(&Extension{Name: "legacy", Hooks: GenerationHooks{
			OnYield: func(ctx context.Context, message MessageReceipt, api *HookAPI) (*Input, error) {
				conv := api.ConversationID()
				var liveID ID
				if err := api.Commit(ctx, func(tx *Tx, rec TaskRecord) (*TaskState, error) {
					for _, doc := range tx.state.Documents {
						if !doc.Retired && doc.Kind == "pi.live" && doc.Owner == conv {
							liveID = doc.ID
						}
					}
					if liveID == 0 {
						t.Error("no live document inside the hook commit")
						return nil, nil
					}
					handle, err := tx.documentRecord(liveID)
					if err != nil {
						t.Errorf("hook cannot open live document %d: %v", liveID, err)
						return nil, nil
					}
					if err := handle.Update(func(value JSON) error {
						run := generationObject(value["run"])
						t.Logf("live doc inside the hook commit: %v", value)
						if len(run) == 0 || run["first"] == nil {
							t.Error("run had no recorded first entry before the mutation, so the fixture proves nothing")
							return nil
						}
						before = run["first"]
						delete(run, "first")
						mutated = true
						return nil
					}); err != nil {
						t.Errorf("hook cannot update live document: %v", err)
						return nil, nil
					}
					return nil, nil
				}); err != nil {
					t.Errorf("hook commit: %v", err)
				}
				return nil, nil
			},
			AfterRun: func(ctx context.Context, run RunSummary, api *HookAPI) (*AfterRunResult, error) {
				seen = run
				return nil, nil
			},
		}}); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			ch <- terminal("done")
			close(ch)
			return ch
		})
		options.Registry = registry
		h = openHarness(t, b.store, options)
		c := root(t, h, ref)
		if _, err := c.Submit(bg, Input{Content: "go"}); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if err := c.WaitForIdle(ctx); err != nil {
			t.Fatal(err)
		}
		if !mutated {
			t.Fatal("onYield hook never mutated the live document; the asserted condition was never established")
		}
		if before == nil {
			t.Fatal("fixture did not capture the recorded first entry before removal")
		}
		if seen.Answer == 0 || seen.First != seen.Answer || seen.Last != seen.Answer {
			t.Fatalf("run without a recorded first entry must answer as first: %+v", seen)
		}
		if err := h.Close(bg); err != nil {
			t.Error(err)
		}
	})
}
