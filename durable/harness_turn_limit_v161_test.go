package durable

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// harness-turn-limit.test.ts:73–94 and 108–122. Only tool rounds count;
// a yield continuation starts a fresh count without settling the input.
func TestSourceV161TurnLimitAtToolRoundAndYield(t *testing.T) {
	for _, tc := range []struct {
		name       string
		limit      int
		steps      []goai.Event
		calls      int64
		yields     int64
		wantDetail string
	}{
		{"first-round", 1, []goai.Event{toolAnswer("c1", "noop", JSON{}), terminal("done")}, 1, 0, "1 turn"},
		{"yield-reset", 2, []goai.Event{toolAnswer("c1", "noop", JSON{}), terminal("done"), toolAnswer("c2", "noop", JSON{}), toolAnswer("c3", "noop", JSON{}), terminal("done")}, 4, 1, "2 turns"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			var calls, yields, reports atomic.Int64
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				return turnLimitResponse(tc.steps, calls.Add(1))
			})
			options.Registry = NewRegistry()
			options.OnReport = func(error) { reports.Add(1) }
			if err := options.Registry.Install(&Extension{Name: "test-turn-limit", Tools: []ToolRegistration{wrapRegistration("noop")}, Hooks: GenerationHooks{
				OnYield: func(context.Context, MessageReceipt, *HookAPI) (*Input, error) {
					if yields.Add(1) == 1 && tc.name == "yield-reset" {
						return &Input{Content: "keep going"}, nil
					}
					return nil, nil
				},
			}}); err != nil {
				t.Fatal(err)
			}
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			if err := c.ConfigurePatch(ctx, AgentPatch{MaxTurns: &tc.limit}); err != nil {
				t.Fatal(err)
			}
			input, err := c.Submit(ctx, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			settled, err := input.Wait(ctx)
			checkSourceTurnLimit(t, settled, err, tc.wantDetail)
			if calls.Load() != tc.calls || yields.Load() != tc.yields {
				t.Fatal("turn count did not stop at the required boundary", calls.Load(), yields.Load(), tc.calls, tc.yields)
			}
			page, err := c.Entries(ctx, EntryQuery{Order: Ascending}, 16, nil)
			if err != nil || page.Next != nil || len(page.Items) == 0 || page.Items[len(page.Items)-1].Kind != "pi.tool-result" {
				t.Fatal("limited run did not end on its tool results", page, err)
			}
			live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil)
			if err != nil || !found {
				t.Fatal("missing LiveDoc after reaching limit", found, err)
			}
			public := generationObject(live.Copy())
			failure := generationObject(public["runFailure"])
			if public["run"] != nil || failure["reason"] != "turn_limit" || failure["detail"] != tc.wantDetail || !slices.Equal(generationTaskIDs(failure["inputs"]), []ID{input.ID()}) {
				t.Fatal("wrong public turn-limit failure", public)
			}
			generation, found, err := h.Task(ctx, submissionEntryID(failure["taskId"]))
			if err != nil || !found || generation.Kind != "pi.generation" || generation.State.Status != "terminal" || generation.State.Outcome == nil || generation.State.Outcome.Status != "completed" {
				t.Fatal("limited generation itself did not complete", generation, found, err)
			}
			if err := h.Close(ctx); err != nil || reports.Load() != 0 {
				t.Fatal("turn limit reported a task fault", reports.Load(), err)
			}
		})
	}
}

// harness-turn-limit.test.ts:192–229. A committed change while a tool runs
// applies at the end of that round; clearing restores the unlimited path.
func TestSourceV161TurnLimitChangesWhileRoundRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	defer releaseTaskGate(release)
	var calls, rounds, unlimitedCalls atomic.Int64
	var cleared atomic.Bool
	limited := []goai.Event{toolAnswer("c1", "noop", JSON{}), toolAnswer("c2", "noop", JSON{}), toolAnswer("c3", "noop", JSON{}), terminal("done")}
	unlimited := []goai.Event{toolAnswer("c4", "noop", JSON{}), toolAnswer("c5", "noop", JSON{}), terminal("done")}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		if cleared.Load() {
			return turnLimitResponse(unlimited, unlimitedCalls.Add(1))
		}
		return turnLimitResponse(limited, calls.Add(1))
	})
	noop := wrapRegistration("noop")
	noop.Execute = func(run context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
		if rounds.Add(1) == 2 {
			close(entered)
			select {
			case <-release:
			case <-run.Done():
				return ToolResult{}, run.Err()
			}
		}
		return ToolResult{}, nil
	}
	options.Registry = NewRegistry()
	if err := options.Registry.Register(noop); err != nil {
		t.Fatal(err)
	}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	for _, invalid := range []int{0, -1} {
		if err := c.ConfigurePatch(ctx, AgentPatch{MaxTurns: &invalid}); err == nil {
			t.Fatal("accepted invalid turn limit", invalid)
		}
	}
	limit := 3
	if err := c.ConfigurePatch(ctx, AgentPatch{MaxTurns: &limit}); err != nil {
		t.Fatal(err)
	}
	limit = 5 // The committed configuration must not retain this caller pointer.
	agent, err := c.Agent(ctx)
	if err != nil || agent.Configuration.MaxTurns == nil || *agent.Configuration.MaxTurns != 3 {
		t.Fatal("stored turn limit lost or aliased", agent.Configuration.MaxTurns, err)
	}
	if err := c.ConfigurePatch(ctx, AgentPatch{MaxTurns: &limit}); err != nil {
		t.Fatal(err)
	}
	limit = 1
	input, err := c.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, entered)
	if err := c.ConfigurePatch(ctx, AgentPatch{MaxTurns: &limit}); err != nil {
		t.Fatal(err)
	}
	if status, err := input.Status(ctx); err != nil || status.Status != "placed" || calls.Load() != 2 {
		t.Fatal("lowering limit ended the in-flight round early", status, calls.Load(), err)
	}
	releaseTaskGate(release)
	settled, err := input.Wait(ctx)
	checkSourceTurnLimit(t, settled, err, "2 turns")
	if calls.Load() != 2 || rounds.Load() != 2 {
		t.Fatal("changed turn limit missed the round boundary", calls.Load(), rounds.Load())
	}
	if err := c.ConfigurePatch(ctx, AgentPatch{Clear: []string{"maxTurns"}}); err != nil {
		t.Fatal(err)
	}
	agent, err = c.Agent(ctx)
	if err != nil || agent.Configuration.MaxTurns != nil {
		t.Fatal("clearing did not remove the turn limit", agent.Configuration.MaxTurns, err)
	}
	cleared.Store(true)
	again, err := c.Submit(ctx, Input{Content: "again"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := again.Wait(ctx)
	if err != nil || result.Record.Status != "done" || unlimitedCalls.Load() != 3 || rounds.Load() != 4 {
		t.Fatal("cleared limit still affected the next run", result.Record, unlimitedCalls.Load(), rounds.Load(), err)
	}
}

func turnLimitResponse(steps []goai.Event, call int64) <-chan goai.Event {
	out := make(chan goai.Event, 1)
	if call <= int64(len(steps)) {
		out <- steps[call-1]
	} else {
		out <- terminal("unexpected request")
	}
	close(out)
	return out
}

func checkSourceTurnLimit(t *testing.T, got Settlement, err error, detail string) {
	t.Helper()
	record := got.Record
	if err != nil || record.Status != "unanswered" || record.Reason != "turn_limit" || record.Answer != 0 || record.Detail == nil || !record.Detail.Present || record.Detail.Value != detail {
		t.Fatalf("submission at turn limit = %+v, %v; want unanswered turn_limit with detail %q", record, err, detail)
	}
}
