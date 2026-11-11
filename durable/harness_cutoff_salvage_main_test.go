package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// f9a26365 harness-restart-failure.test.ts621/648. Real streaming progress and
// failed-host reopen; only source648 explicitly seeds restarts=2 in raw storage.
func mainSalvageCrash(t *testing.T, b sqliteFixture, limit uint64) (*mainRecoveryHarness, Options, ModelRef, ID, ID, Storage, chan *mainRecoveryStream) {
	t.Helper()
	ref, options, streams, clock := mainRecoverySetup(t)
	zero := int64(0)
	window := float64(0)
	options.Now = clock.Load
	options.Settings = &HarnessSettings{RestartLimit: &limit, Progress: &ProgressSettings{PartialIntervalMs: &zero, PersistWithinMs: &window}}
	h := openHarness(t, b.store, options)
	c := root(t, h, ref)
	_, frames := recordUpstreamWindow(t, c)
	f := &mainRecoveryHarness{t: t, h: h, c: c, store: b.store, clock: clock, frames: frames}
	sub, err := c.Submit(bg, Input{Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	stream := watchReceive(t, streams)
	// A memory-window update is not evidence that progress survived storage.
	// This recovery fixture needs a committed partial before simulating loss.
	storedPartial := make(chan struct{})
	var storedOnce sync.Once
	observation, err := h.session.observeStoredCommits(sessionCommitObserver{advance: func(_ context.Context, p sessionPublication) error {
		for _, change := range p.Documents {
			if change.Record.Kind == "pi.live" && change.Record.Owner == c.ID() {
				if text, ok := mainRecoveryText(change.Value); ok && text == strings.Repeat("partial ", 12) {
					storedOnce.Do(func() { close(storedPartial) })
				}
			}
		}
		return nil
	}, end: func(WatchEnd) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	defer observation.detach()
	f.partial(stream, strings.Repeat("partial ", 12))
	awaitTaskSignal(t, storedPartial)
	task, cp, ok, err := h.nextTask(c.ID())
	if err != nil || !ok || cp.Phase != "intent" {
		t.Fatal("stream fixture", task, cp, err)
	}
	store := f.crash()
	return f, options, ref, sub.ID(), task.ID, store, streams
}
func mainSalvageOpen(t *testing.T, store Storage, options Options, conversation ID) (*Harness, *ConversationHandle) {
	t.Helper()
	h := openHarness(t, store, options)
	c, err := h.Conversation(bg, conversation)
	if err != nil {
		t.Fatal(err)
	}
	return h, c
}
func mainSalvageRoles(t *testing.T, state Snapshot, c ID) []goai.Role {
	t.Helper()
	var roles []goai.Role
	for _, id := range ids(state.Entries) {
		e := state.Entries[id]
		if e.Conversation == c && isMessageEntry(e) {
			var m MessageReceipt
			if err := decodeEntryMessage(e, DefaultLimits(), &m); err != nil {
				t.Fatal(err)
			}
			roles = append(roles, m.Role)
		}
	}
	return roles
}
func mainSalvageCheck(t *testing.T, h *Harness, c *ConversationHandle, sub, task ID, detail string, keep bool) {
	t.Helper()
	if err := h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	ended := mainCutoffTask(t, h, task)
	mainCutoffOutcome(t, ended, detail)
	status := mainCutoffStatus(t, h, sub)
	if status.Status != "unanswered" || status.Reason != "restart_limit" || (status.Detail == nil || !status.Detail.Present || !equalJSONValue(status.Detail.Value, detail)) {
		t.Error("settled", status)
	}
	state, err := h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	live := mainAbortFailureLive(t, state, c.ID())
	if live["generation"] != nil {
		t.Error("generation retained", live)
	}
	wantRoles := []goai.Role{goai.RoleUser}
	if keep {
		wantRoles = append(wantRoles, goai.RoleAssistant)
	}
	if roles := mainSalvageRoles(t, state, c.ID()); !reflect.DeepEqual(roles, wantRoles) {
		t.Error("entries", roles, wantRoles)
	}
	if keep {
		for _, e := range state.Entries {
			if e.ByTask == task && messageEntryValue(e)["role"] == string(goai.RoleAssistant) {
				var m MessageReceipt
				if err := decodeEntryMessage(e, h.session.limits, &m); err != nil {
					t.Fatal(err)
				}
				if len(m.Content) != 1 || m.Content[0].Text != strings.Repeat("partial ", 12) || m.StopReason != goai.StopReasonAborted {
					t.Error("partial", m)
				}
			}
		}
	}
	// Native failedRun is a documented extra compatibility flag, not a
	// source field. Assert it explicitly; no other extra live keys are allowed.
	if len(live) != 2 || live["failedRun"] != true {
		t.Error("native live failure shape", live)
	}
	failure := generationObject(live["runFailure"])
	if len(failure) != 5 || failure["reason"] != "restart_limit" || failure["detail"] != detail || submissionEntryID(failure["taskId"]) != task {
		t.Error("run failure", failure)
	}
	inputs, _ := failure["inputs"].([]any)
	if len(inputs) != 1 || submissionEntryID(inputs[0]) != sub || submissionEntryID(failure["first"]) != status.Entry {
		t.Error("failure ownership", failure, status)
	}
}
func TestMainCutoffKeepsCommittedPartialAtLimit(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f, options, _, sub, task, store, streams := mainSalvageCrash(t, b, 1)
		h, c := mainSalvageOpen(t, store, options, f.c.ID())
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if text, ok := mainRecoveryText(mainAbortFailureLive(t, state, c.ID())); !ok || text != strings.Repeat("partial ", 12) {
			t.Fatal("stored partial not reopened", text, ok)
		}
		mainSalvageCheck(t, h, c, sub, task, "The generation was interrupted once in its request phase", true)
		if len(streams) != 0 {
			t.Error("cutoff called model again")
		}
	})
}
func TestMainCutoffDropsPartialPastLimit(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		f, options, _, sub, id, store, streams := mainSalvageCrash(t, b, 2)
		state, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		task := state.Tasks[id]
		task.Restarts = 2
		if _, err := store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &task}}}); err != nil {
			t.Fatal(err)
		}
		h, c := mainSalvageOpen(t, store, options, f.c.ID())
		record, _, err := h.Task(bg, id)
		if err != nil || record.Restarts != 3 {
			t.Fatal("source648 fixture", record, err)
		}
		mainSalvageCheck(t, h, c, sub, id, "The generation was interrupted 3 times in a row in its request phase", false)
		if len(streams) != 0 {
			t.Error("cutoff called model again")
		}
	})
}

// Source-derived scheduler.ts1933 ordering. Native registry has no snapshot
// callback; holding its read-lock dependency makes premature snapshots visible.
// A seeded cutoff must fault without waiting for definition/snapshot resolution.
func TestMainCutoffBeforeRegistrySnapshotDerived(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var runs atomic.Int64
		def := taskDefinition(t, "task.cutoff-order", func(context.Context, TaskRecord, *TaskRuntime) error { runs.Add(1); return nil })
		limit := uint64(1)
		h := taskTestHarnessOptions(t, b.store, Options{Settings: &HarnessSettings{RestartLimit: &limit}}, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if _, err := h.session.Commit(bg, func(tx *Tx) error {
			task := tx.state.Tasks[id]
			task.Restarts = 1
			return tx.stage(Write{Op: "put-task", Task: &task})
		}); err != nil {
			t.Fatal(err)
		}
		h.options.Registry.mu.Lock()
		if err := h.Resume(bg); err != nil {
			h.options.Registry.mu.Unlock()
			t.Fatal(err)
		}
		type result struct {
			record TaskRecord
			err    error
		}
		done := make(chan result, 1)
		go func() { record, err := h.WaitForTask(bg, id); done <- result{record, err} }()
		var got result
		select {
		case got = <-done:
			h.options.Registry.mu.Unlock()
		case <-time.After(time.Second):
			h.options.Registry.mu.Unlock()
			t.Error("cutoff waited for registry snapshot")
			got = watchReceive(t, done)
		}
		record := got.record
		if got.err != nil {
			t.Fatal(got.err)
		}
		if record.State.Outcome == nil || record.State.Outcome.Status != "faulted" || runs.Load() != 0 {
			t.Error("cutoff resolved/executed definition", record, runs.Load())
		}
	})
}

// Source-derived generation.ts endCutOff/deferredOf: poll and retry-of-poll
// cancel only at the salvageable boundary. Phase/handle/restart seeds are native
// codec controls after a real request owns a run, not translated crash scenarios.
// Preserve its partial through the supported legacy checkpoint adapter; these
// seeded phases do not establish modern poll/retry presentation persistence.
func TestMainCutoffDeferredSalvagePhasesDerived(t *testing.T) {
	for _, phase := range []string{"poll", "retry"} {
		for _, past := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/past=%v", phase, past), func(t *testing.T) {
				sqliteStore(t, func(t *testing.T, b sqliteFixture) {
					f, options, ref, sub, id, store, _ := mainSalvageCrash(t, b, 1)
					var cancels atomic.Int64
					api := options.Models(ref.Provider, ref.ID).Api
					original := goai.GetApiProvider(api)
					provider := *original
					provider.CancelDeferred = func(_ context.Context, _ *goai.Model, handle goai.DeferredHandle, _ *goai.StreamOptions) error {
						if handle.ID != "job" {
							t.Error("wrong handle", handle)
						}
						cancels.Add(1)
						return nil
					}
					goai.RegisterApi(&provider)
					state, err := store.Snapshot(bg)
					if err != nil {
						t.Fatal(err)
					}
					task := state.Tasks[id]
					var cp generationCheckpoint
					if err := fromObject(task.Checkpoint, &cp, DefaultLimits()); err != nil {
						t.Fatal(err)
					}
					cp.Partial, err = generationPartial(state, task, cp, DefaultLimits())
					if err != nil || cp.Partial == nil {
						t.Fatal("legacy partial adapter", err)
					}
					cp.Phase = phase
					cp.Deferred = &goai.DeferredHandle{ID: "job"}
					cp.PollAt = 0
					cp.RetryUntil = 0
					task.Checkpoint, err = dtoObject(cp, DefaultLimits())
					if err != nil {
						t.Fatal(err)
					}
					if past {
						task.Restarts = 1
					}
					if _, err := store.Apply(bg, Batch{Writes: []Write{{Op: "put-task", Task: &task}}}); err != nil {
						t.Fatal(err)
					}
					h, c := mainSalvageOpen(t, store, options, f.c.ID())
					count := "once"
					want := int64(1)
					if past {
						count = "2 times in a row"
						want = 0
					}
					mainSalvageCheck(t, h, c, sub, id, "The generation was interrupted "+count+" in its "+phase+" phase", !past)
					if cancels.Load() != want {
						t.Error("deferred salvage cancellation", cancels.Load(), want)
					}
				})
			})
		}
	}
}
