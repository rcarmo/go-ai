package durable

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// harness-turn-limit.test.ts:97–105. The answer after one tool round is done;
// a new input starts a fresh count and stops after two of its own rounds.
func TestSourceV161TurnLimitIndependentRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	steps := []goai.Event{toolAnswer("c1", "noop", JSON{}), terminal("done"), toolAnswer("c2", "noop", JSON{}), toolAnswer("c3", "noop", JSON{}), terminal("unexpected request")}
	_, c, calls := turnLimitInboxHarness(t, ctx, 2, "", steps, nil)
	first := submitTurnLimitInput(t, ctx, c, Input{Content: "one"})
	answer, err := first.Wait(ctx)
	if err != nil || answer.Record.Status != "done" || answer.Record.Answer == 0 || calls.Load() != 2 {
		t.Fatal("first run did not answer after one tool round", answer.Record, calls.Load(), err)
	}
	second := submitTurnLimitInput(t, ctx, c, Input{Content: "two"})
	stopped, err := second.Wait(ctx)
	checkSourceTurnLimit(t, stopped, err, "2 turns")
	if calls.Load() != 4 {
		t.Fatal("next run did not use two fresh tool rounds", calls.Load())
	}
	if got, err := first.Status(ctx); err != nil || got.Status != "done" || got.Answer != answer.Record.Answer {
		t.Fatal("second run changed the already answered input", got, err)
	}
}

// harness-turn-limit.test.ts:125–157. A steer joins at postTools before
// reaching the limit, so both placed inputs settle together at the cutoff.
func TestSourceV161TurnLimitSettlesPlacedSteer(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	entered := make(chan int64, 2)
	gates := []chan struct{}{make(chan struct{}), make(chan struct{})}
	defer releaseTaskGate(gates[0])
	defer releaseTaskGate(gates[1])
	var rounds atomic.Int64
	steps := []goai.Event{toolAnswer("c1", "noop", JSON{}), toolAnswer("c2", "noop", JSON{}), terminal("unexpected request")}
	_, c, calls := turnLimitInboxHarness(t, ctx, 2, "", steps, func(run context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
		n := rounds.Add(1)
		if n <= 2 {
			entered <- n
			select {
			case <-gates[n-1]:
			case <-run.Done():
				return ToolResult{}, run.Err()
			}
		}
		return ToolResult{}, nil
	})
	first := submitTurnLimitInput(t, ctx, c, Input{Content: "go"})
	if n := watchReceive(t, entered); n != 1 {
		t.Fatal("did not reach first held round", n)
	}
	steer := submitTurnLimitInput(t, ctx, c, Input{Content: "also this", WhenBusy: "steer"})
	if record, err := steer.Status(ctx); err != nil || record.Status != "queued" || record.Entry != 0 {
		t.Fatal("steer was not queued and unplaced during the first round", record, err)
	}
	releaseTaskGate(gates[0])
	if n := watchReceive(t, entered); n != 2 {
		t.Fatal("did not reach second held round", n)
	}
	want := []ID{first.ID(), steer.ID()}
	live := turnLimitInboxLive(t, ctx, c)
	run := generationObject(live["run"])
	if record, err := steer.Status(ctx); err != nil || record.Status != "placed" || record.Entry == 0 || !slices.Equal(generationTaskIDs(run["inputs"]), want) {
		t.Fatal("steer did not join the original run before the limiting round", record, run, err)
	}
	releaseTaskGate(gates[1])
	for _, input := range []*SubmissionHandle{first, steer} {
		stopped, err := input.Wait(ctx)
		checkSourceTurnLimit(t, stopped, err, "2 turns")
	}
	live = turnLimitInboxLive(t, ctx, c)
	failure := generationObject(live["runFailure"])
	if got := generationTaskIDs(failure["inputs"]); !slices.Equal(got, want) {
		t.Fatal("turn-limit failure lost its ordered inputs", got, want)
	}
	if calls.Load() != 2 {
		t.Fatal("limiting round made an extra request", calls.Load())
	}
}

// harness-turn-limit.test.ts:160–189. A queued follow-up was not placed into
// the capped run; the selected failed-run policy alone chooses its disposition.
func TestSourceV161TurnLimitQueuedPolicy(t *testing.T) {
	for _, policy := range []string{"keep", "continue", "withdraw"} {
		t.Run(policy, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			defer releaseTaskGate(release)
			steps := []goai.Event{toolAnswer("c1", "noop", JSON{}), terminal("done")}
			var effects atomic.Int64
			h, c, calls := turnLimitInboxHarness(t, ctx, 1, policy, steps, func(run context.Context, _ any, _ *ToolAPI) (ToolResult, error) {
				if effects.Add(1) == 1 {
					close(entered)
					select {
					case <-release:
					case <-run.Done():
						return ToolResult{}, run.Err()
					}
				}
				return ToolResult{}, nil
			})
			first := submitTurnLimitInput(t, ctx, c, Input{Content: "go"})
			awaitTaskSignal(t, entered)
			follow := submitTurnLimitInput(t, ctx, c, Input{Content: "also", WhenBusy: "followUp"})
			if queued, err := follow.Status(ctx); err != nil || queued.Status != "queued" || queued.Entry != 0 || queued.Answer != 0 {
				t.Fatal("follow-up was not queued during the limiting tool round", queued, err)
			}
			releaseTaskGate(release)
			stopped, err := first.Wait(ctx)
			checkSourceTurnLimit(t, stopped, err, "1 turn")
			if err := h.WaitForIdle(ctx); err != nil {
				t.Fatal(err)
			}
			queued, err := follow.Status(ctx)
			if err != nil {
				t.Fatal(err)
			}
			switch policy {
			case "keep":
				if queued.Status != "queued" || queued.Entry != 0 || queued.Answer != 0 || calls.Load() != 1 {
					t.Fatal("keep did not retain the queued follow-up", queued, calls.Load())
				}
			case "continue":
				if queued.Status != "done" || queued.Entry == 0 || queued.Answer == 0 || calls.Load() != 2 {
					t.Fatal("continue did not run the queued follow-up", queued, calls.Load())
				}
			case "withdraw":
				if queued.Status != "unanswered" || queued.Reason != "run_failed" || queued.Entry != 0 || queued.Answer != 0 || calls.Load() != 1 {
					t.Fatal("withdraw did not settle the queued follow-up as run_failed", queued, calls.Load())
				}
			}
		})
	}
}

func turnLimitInboxHarness(t *testing.T, ctx context.Context, limit int, policy string, steps []goai.Event, execute func(context.Context, any, *ToolAPI) (ToolResult, error)) (*Harness, *ConversationHandle, *atomic.Int64) {
	t.Helper()
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		return turnLimitResponse(steps, calls.Add(1))
	})
	options.Registry = NewRegistry()
	options.Settings = &HarnessSettings{AfterFailedRun: policy}
	noop := wrapRegistration("noop")
	if execute != nil {
		noop.Execute = execute
	}
	if err := options.Registry.Register(noop); err != nil {
		t.Fatal(err)
	}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	if err := c.ConfigurePatch(ctx, AgentPatch{MaxTurns: &limit}); err != nil {
		t.Fatal(err)
	}
	return h, c, &calls
}

func submitTurnLimitInput(t *testing.T, ctx context.Context, c *ConversationHandle, input Input) *SubmissionHandle {
	t.Helper()
	submission, err := c.Submit(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return submission
}

func turnLimitInboxLive(t *testing.T, ctx context.Context, c *ConversationHandle) JSON {
	t.Helper()
	live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil)
	if err != nil || !found {
		t.Fatal("missing LiveDoc", found, err)
	}
	return generationObject(live.Copy())
}
