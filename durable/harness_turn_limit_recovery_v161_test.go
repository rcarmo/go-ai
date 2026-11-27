package durable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// harness-turn-limit.test.ts:273–304, through actual SQLite-owned creation and
// a historical ownerless fork. The caller's budget is not mutated by either.
func TestSourceV161TurnLimitInheritedAndOverridden(t *testing.T) {
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
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	parent := root(t, h, ref)
	four, two := 4, 2
	if err := parent.ConfigurePatch(ctx, AgentPatch{MaxTurns: &four}); err != nil {
		t.Fatal(err)
	}
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

// harness-turn-limit.test.ts:307–358. Upstream cuts the storage proxy; the
// native witness kills the process while its second real tool effect is held.
func TestSourceV161TurnLimitPersistsAcrossRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const cutoffDir = "GOAI_TEST_V161_TURN_LIMIT_CUTOFF"
	type cutoff struct{ Conversation, Submission, Tool, Generation ID }
	readRun := func(c *ConversationHandle) ID {
		t.Helper()
		live, found, err := c.SnapshotDefinition(ctx, LiveDoc(), nil)
		if err != nil || !found {
			t.Fatal("missing running conversation", found, err)
		}
		run := generationObject(generationObject(live.Copy())["run"])
		if got := submissionEntryID(run["turns"]); got != 2 {
			t.Fatalf("stored turn count = %d; want 2", got)
		}
		return submissionEntryID(run["taskId"])
	}
	setup := func(steps []goai.Event, hold func(int64) bool, release <-chan struct{}) (ModelRef, Options, <-chan ID, *atomic.Int64, *atomic.Int64) {
		var calls, runs atomic.Int64
		entered := make(chan ID, 1)
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			return turnLimitResponse(steps, calls.Add(1))
		})
		noop := wrapRegistration("noop") // Replay-safe with the same registered implementation on both sides.
		noop.Execute = func(run context.Context, _ any, api *ToolAPI) (ToolResult, error) {
			if hold(runs.Add(1)) {
				entered <- api.TaskID()
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
		return ref, options, entered, &calls, &runs
	}
	if dir := os.Getenv(cutoffDir); dir != "" {
		store, err := openSQLiteTestDirectory(filepath.Join(dir, "db"), SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		ref, options, entered, requests, _ := setup([]goai.Event{toolAnswer("c1", "noop", JSON{}), toolAnswer("c2", "noop", JSON{})}, func(n int64) bool { return n == 2 }, nil)
		h := openHarness(t, store, options)
		c := root(t, h, ref)
		two := 2
		if err := c.ConfigurePatch(ctx, AgentPatch{MaxTurns: &two}); err != nil {
			t.Fatal(err)
		}
		input, err := c.Submit(ctx, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		child := watchReceive(t, entered)
		generation := readRun(c)
		if generation == 0 || requests.Load() != 2 {
			t.Fatal("wrong cutoff generation or request count", generation, requests.Load())
		}
		data, err := json.Marshal(cutoff{c.ID(), input.ID(), child, generation})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cutoff.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		t.Fatal("SIGKILL did not stop the original writer", syscall.Kill(os.Getpid(), syscall.SIGKILL))
	}

	dir := t.TempDir()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceV161TurnLimitPersistsAcrossRestart$")
	cmd.Env = append(os.Environ(), cutoffDir+"="+dir)
	output, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || ctx.Err() != nil {
		t.Fatalf("writer did not reach cutoff: %v\n%s", err, output)
	}
	if status, ok := exit.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
		t.Fatalf("writer did not exit via SIGKILL: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(dir, "cutoff.json"))
	if err != nil {
		t.Fatal(err)
	}
	var witness cutoff
	if err := json.Unmarshal(data, &witness); err != nil || witness.Tool == 0 || witness.Generation == 0 {
		t.Fatal("invalid cutoff receipt", witness, err)
	}
	store, err := openSQLiteTestDirectory(filepath.Join(dir, "db"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	before, found, err := store.Task(ctx, witness.Tool)
	if err != nil || !found || before.Owner != witness.Generation || before.State.Status != "running" {
		t.Fatal("cutoff did not preserve the real running tool", before, found, err)
	}
	release := make(chan struct{})
	defer releaseTaskGate(release)
	_, options, entered, requests, runs := setup([]goai.Event{terminal("unexpected retry")}, func(n int64) bool { return n == 1 }, release)
	h := openHarness(t, store, options)
	c, err := h.Conversation(ctx, witness.Conversation)
	if err != nil || c == nil {
		t.Fatal("could not reopen conversation", c, err)
	}
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	if id := watchReceive(t, entered); id != witness.Tool || readRun(c) != witness.Generation {
		t.Fatal("replay did not preserve the active tool and generation", id, witness)
	}
	input, err := h.Submission(ctx, witness.Submission)
	if err != nil || input == nil {
		t.Fatal("missing original submission", input, err)
	}
	releaseTaskGate(release)
	result, err := input.Wait(ctx)
	checkSourceTurnLimit(t, result, err, "2 turns")
	if requests.Load() != 0 || runs.Load() != 1 {
		t.Fatal("reopen replayed a provider request or more than one tool", requests.Load(), runs.Load())
	}
}

// harness-turn-limit.test.ts:232–248. Explicit termination takes precedence;
// handoff alone places its reset but does not bypass the round limit.
func TestSourceV161TurnLimitTerminationAndHandoff(t *testing.T) {
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
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	one := 1
	if err := c.ConfigurePatch(ctx, AgentPatch{MaxTurns: &one}); err != nil {
		t.Fatal(err)
	}
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
func TestSourceV161TurnLimitContinueOnToolResults(t *testing.T) {
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
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	c := root(t, h, ref)
	one := 1
	if err := c.ConfigurePatch(ctx, AgentPatch{MaxTurns: &one}); err != nil {
		t.Fatal(err)
	}
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
