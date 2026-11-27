package durable

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source locators refer to official pi v1.6.1 (ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe).
// The v1.7.0 turn-limit suite has the same source bytes.

// harness-turn-limit.test.ts:73–94 and 108–122. Only tool rounds count;
// a yield continuation starts a fresh count without settling the input.
func TestTurnLimitAtToolRoundAndYield(t *testing.T) {
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
			h, c, _ := openSQLiteTurnLimitHarness(t, ctx, ref, options, tc.limit)
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
func TestTurnLimitChangesWhileRoundRunning(t *testing.T) {
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

// harness-turn-limit.test.ts:273–304, through actual SQLite-owned creation and
// a historical ownerless fork. The caller's budget is not mutated by either.
func TestTurnLimitInheritedAndOverridden(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	var calls atomic.Int64
	steps := []goai.Event{toolAnswer("c1", "spawn", JSON{}), terminal("done")}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		return turnLimitResponse(steps, calls.Add(1))
	})
	type created struct{ child, owner ID }
	child := make(chan created, 1)
	spawn := wrapRegistration("spawn")
	spawn.Execute = func(run context.Context, _ any, api *ToolAPI) (ToolResult, error) {
		c, err := api.CreateOwnedConversation(run, "", nil)
		if err != nil {
			return ToolResult{}, err
		}
		child <- created{c.ID(), api.TaskID()}
		return ToolResult{}, nil
	}
	options.Registry = NewRegistry()
	if err := options.Registry.Register(spawn); err != nil {
		t.Fatal(err)
	}
	h, parent, store := openSQLiteTurnLimitHarness(t, ctx, ref, options, 4)
	two := 2
	input, err := parent.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := input.Wait(ctx)
	if err != nil || answer.Record.Status != "done" || answer.Record.Answer == 0 || calls.Load() != 2 {
		t.Fatal("original run did not answer after spawning", answer.Record, calls.Load(), err)
	}
	spawned := watchReceive(t, child)
	stored, found, err := store.Conversation(ctx, spawned.child)
	if err != nil || !found || stored.Owner != spawned.owner || spawned.owner == 0 {
		t.Fatal("wrong spawned conversation ownership", stored, spawned, found, err)
	}
	nested, err := h.Conversation(ctx, spawned.child)
	if err != nil || nested == nil {
		t.Fatal("missing spawned conversation", nested, err)
	}
	assertLimit := func(c *ConversationHandle, want int) {
		t.Helper()
		a, err := c.Agent(ctx)
		if err != nil || a.Configuration.MaxTurns == nil || *a.Configuration.MaxTurns != want {
			got := 0
			if a.Configuration.MaxTurns != nil {
				got = *a.Configuration.MaxTurns
			}
			t.Fatalf("conversation %d turn limit = %d, %v; want %d", c.ID(), got, err, want)
		}
	}
	assertLimit(nested, 4)
	fork, err := parent.Fork(ctx, answer.Record.Answer)
	if err != nil {
		t.Fatal(err)
	}
	assertLimit(fork, 4)
	own, err := h.CreateConversation(ctx, AgentChange{MaxTurns: &two})
	if err != nil {
		t.Fatal(err)
	}
	assertLimit(own, 2)
	assertLimit(parent, 4)
}

// harness-turn-limit.test.ts:232–248. Explicit termination takes precedence;
// handoff alone places its reset but does not bypass the round limit.
func TestTurnLimitTerminationAndHandoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	var calls atomic.Int64
	steps := []goai.Event{toolAnswer("t", "noop", JSON{}), toolAnswer("h", "noop", JSON{}), terminal("unexpected request")}
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		return turnLimitResponse(steps, calls.Add(1))
	})
	options.Registry = NewRegistry()
	if err := options.Registry.Install(&Extension{Name: "test-turn-limit-control", Tools: []ToolRegistration{wrapRegistration("noop")}, ToolHooks: ToolHooks{
		AfterTool: func(_ context.Context, call goai.ToolCall, result ToolResult, _ *HookAPI) (*ToolResult, error) {
			if call.ID == "t" {
				result.Control = &ToolControl{Terminate: true}
			} else {
				fresh := "fresh"
				result.Control = &ToolControl{Handoff: &fresh}
			}
			return &result, nil
		},
	}}); err != nil {
		t.Fatal(err)
	}
	_, c, _ := openSQLiteTurnLimitHarness(t, ctx, ref, options, 1)
	first, err := c.Submit(ctx, Input{Content: "one"})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := first.Wait(ctx)
	if err != nil || answer.Record.Status != "done" || answer.Record.Answer == 0 || calls.Load() != 1 {
		t.Fatal("termination did not win at the turn limit", answer.Record, calls.Load(), err)
	}
	second, err := c.Submit(ctx, Input{Content: "two"})
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := second.Wait(ctx)
	checkSourceTurnLimit(t, stopped, err, "1 turn")
	page, err := c.Entries(ctx, EntryQuery{Order: Ascending}, 16, nil)
	if err != nil || page.Next != nil || len(page.Items) == 0 || calls.Load() != 2 {
		t.Fatal("handoff issued another provider request", page.Next, calls.Load(), err)
	}
	var resets []Entry
	for _, entry := range page.Items {
		if entry.Kind == "pi.reset" {
			resets = append(resets, entry)
		}
	}
	if len(resets) != 1 || resets[0].ID != page.Items[len(page.Items)-1].ID || resets[0].Head != resets[0].ID || len(resets[0].Model) != 1 {
		t.Fatal("handoff did not place the only reset before cutoff", resets)
	}
	message := receiptMessage(resets[0].Model[0])
	if message.Role != goai.RoleUser || len(message.Content) != 1 || message.Content[0].Text != "fresh" {
		t.Fatal("wrong handoff reset model message", message)
	}
}

// harness-turn-limit.test.ts:251–270. A manual continue gets a fresh round
// budget and its first request uses the old round's committed tool results.
func TestTurnLimitContinueOnToolResults(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	var calls atomic.Int64
	seen := make(chan goai.Role, 1)
	held := make(chan goai.Event, 1)
	closeHeld := sync.OnceFunc(func() { close(held) })
	defer closeHeld() // Harness drains the provider on an assertion failure.
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, request *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		switch calls.Add(1) {
		case 1:
			return turnLimitResponse([]goai.Event{toolAnswer("c1", "noop", JSON{})}, 1)
		case 2:
			var role goai.Role
			if len(request.Messages) != 0 {
				role = request.Messages[len(request.Messages)-1].Role
			}
			seen <- role
			return held
		default:
			return turnLimitResponse(nil, 1)
		}
	})
	options.Registry = NewRegistry()
	if err := options.Registry.Register(wrapRegistration("noop")); err != nil {
		t.Fatal(err)
	}
	h, c, _ := openSQLiteTurnLimitHarness(t, ctx, ref, options, 1)
	first, err := c.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	stopped, err := first.Wait(ctx)
	checkSourceTurnLimit(t, stopped, err, "1 turn")
	continued, err := c.Submit(ctx, Input{Type: "continue"})
	if err != nil {
		t.Fatal(err)
	}
	if role := watchReceive(t, seen); role != goai.RoleToolResult {
		t.Fatalf("continue provider last role = %q; want toolResult", role)
	}
	live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil)
	if err != nil || !found {
		t.Fatal("continue did not have a live run", found, err)
	}
	run := generationObject(generationObject(live.Copy())["run"])
	turns, valid := exactNumber(run["turns"])
	if !valid || turns == nil || turns.Sign() != 0 {
		t.Fatal("continue did not start a fresh turn budget", run)
	}
	held <- terminal("finished")
	closeHeld()
	settled, err := continued.Wait(ctx)
	if err != nil || settled.Record.Status != "done" || settled.Record.Type != "continue" || settled.Record.Answer == 0 || calls.Load() != 2 {
		t.Fatal("continue did not answer once", settled.Record, calls.Load(), err)
	}
	entry, found, err := h.Entry(ctx, settled.Record.Answer)
	if err != nil || !found || len(entry.Model) != 1 || v160ToolResultText(entry.Model[0]) != "finished" {
		t.Fatal("continue settled against the wrong answer", entry, found, err)
	}
}

// harness-turn-limit.test.ts:97–105. The answer after one tool round is done;
// a new input starts a fresh count and stops after two of its own rounds.
func TestTurnLimitIndependentRuns(t *testing.T) {
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
func TestTurnLimitSettlesPlacedSteer(t *testing.T) {
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
func TestTurnLimitQueuedPolicy(t *testing.T) {
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
	h, c, _ := openSQLiteTurnLimitHarness(t, ctx, ref, options, limit)
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

func openSQLiteTurnLimitHarness(t *testing.T, ctx context.Context, ref ModelRef, options Options, maxTurns int) (*Harness, *ConversationHandle, *SQLiteStorage) {
	t.Helper()
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	return h, configureTurnLimitedRoot(t, ctx, h, ref, maxTurns), store
}

func configureTurnLimitedRoot(t *testing.T, ctx context.Context, h *Harness, ref ModelRef, maxTurns int) *ConversationHandle {
	t.Helper()
	c := root(t, h, ref)
	if err := c.ConfigurePatch(ctx, AgentPatch{MaxTurns: &maxTurns}); err != nil {
		t.Fatal(err)
	}
	return c
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
