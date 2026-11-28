package durable

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Source locators refer to official pi v1.7.0 (fb0fe823539f6d04850f6e34748c01f01f212e1f).
// The v1.6.1 turn-limit suite has the same source bytes.

// harness-turn-limit.test.ts:307–358. Upstream cuts the storage proxy; the
// native witness kills the process while its second real tool effect is held.
func TestTurnLimitPersistsAcrossRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const cutoffDir = "GOAI_TEST_TURN_LIMIT_CUTOFF"
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
		c := configureTurnLimitedRoot(t, ctx, h, ref, 2)
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
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTurnLimitPersistsAcrossRestart$")
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
