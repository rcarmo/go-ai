package durable

import (
	"context"
	"errors"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func sleepDefinition140(t *testing.T, deadline int64, after *atomic.Int64, hosts chan<- *TaskRuntime, release <-chan struct{}) *TaskDefinition {
	t.Helper()
	def, err := DefineTask(TaskDefinitionOptions{Kind: "task.sleep-140", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "start"}, nil }, Phases: map[string]TaskPhase{
		"start": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			hosts <- r
			checkpoint := JSON{"phase": "after"}
			if err := r.SleepUntil(ctx, deadline, checkpoint); err != nil {
				return err
			}
			checkpoint["phase"] = "start" // supplied state must be detached at adoption
			if !errors.Is(r.check(), ErrSealed) {
				return errors.New("sleep retained invocation capabilities")
			}
			if release != nil {
				<-release
			}
			return nil
		},
		"after": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			after.Add(1)
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("awake"), nil })
		},
	}, Abort: func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) {
			return &TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "aborted"}}, nil
		})
	}})
	if err != nil {
		t.Fatal(err)
	}
	return def
}
func awaitSleeping140(t *testing.T, h *Harness, id ID) {
	t.Helper()
	state, sub, err := h.session.SubscribeCommits(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Stop()
	if state.Tasks[id].Status == "sleeping" {
		return
	}
	reached := make(chan struct{}, 1)
	if err = sub.Start(func(_ context.Context, frame PublicationFrame) error {
		if frame.Snapshot != nil && frame.Snapshot.Tasks[id].Status == "sleeping" {
			select {
			case reached <- struct{}{}:
			default:
			}
		}
		for _, write := range frame.Publication.Tables {
			if write.Task != nil && write.Task.ID == id && write.Task.Status == "sleeping" {
				select {
				case reached <- struct{}{}:
				default:
				}
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, reached)
}
func TestSleep140PersistedContinuationReopenAndAbort(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var clock, after atomic.Int64
		clock.Store(1000)
		hosts := make(chan *TaskRuntime, 4)
		def := sleepDefinition140(t, 1000000, &after, hosts, nil)
		options := Options{Now: clock.Load}
		h := taskTestHarnessOptions(t, b.store, options, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitSleeping140(t, h, id)
		r := <-hosts
		awaitTaskSignal(t, r.done)
		inspection, err := h.Inspect(bg)
		if err != nil {
			t.Fatal(err)
		}
		if len(inspection.Tasks) != 1 || inspection.Tasks[0].Kind != "sleeping" || inspection.Tasks[0].Until == nil || *inspection.Tasks[0].Until != 1000000 {
			t.Fatal(inspection)
		}
		graph, err := h.TaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		key := strconv.FormatUint(uint64(id), 10)
		if graph.Tasks[key].State.Status != "sleeping" || graph.Tasks[key].State.Phase != "after" || *graph.Tasks[key].State.Until != 1000000 {
			t.Fatal(graph)
		}
		*graph.Tasks[key].State.Until = 1
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h2 := taskTestHarnessOptions(t, b.reopen(), options, def)
		state, err := h2.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if state.Tasks[id].Status != "sleeping" || state.Tasks[id].Restarts != 0 || *state.Tasks[id].Execution.Native.State.Until != 1000000 {
			t.Fatal(state.Tasks[id])
		}
		if err = h2.Resume(bg); err != nil {
			t.Fatal(err)
		}
		if err = h2.session.readTasks(bg, func(state Snapshot) error {
			if h2.scheduler.runnable(state, state.Tasks[id]) {
				t.Error("sleep admitted early")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		clock.Store(1000000)
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		receipt, err := h2.WaitForTask(ctx, id)
		if err != nil || receipt.State.Outcome.Status != "completed" || after.Load() != 1 {
			t.Fatal(receipt, err, after.Load())
		}
		clock.Store(1000)
		aborted := createPublicTask(t, h2, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h2.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitSleeping140(t, h2, aborted)
		if _, err = h2.AbortTask(bg, aborted); err != nil {
			t.Fatal(err)
		}
		receipt, err = h2.WaitForTask(ctx, aborted)
		if err != nil || receipt.State.Outcome.Status != "aborted" || after.Load() != 1 {
			t.Fatal(receipt, err)
		}
	})
}
func TestSleep140RetainsActualPermitUntilHostReturns(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var clock, after atomic.Int64
		clock.Store(1000)
		hosts := make(chan *TaskRuntime, 1)
		release := make(chan struct{})
		defer func() {
			select {
			case <-release:
			default:
				close(release)
			}
		}()
		def := sleepDefinition140(t, 2000, &after, hosts, release)
		h := taskTestHarnessOptions(t, b.store, Options{Now: clock.Load}, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitSleeping140(t, h, id)
		runtime := <-hosts
		clock.Store(2000)
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		if err := h.session.readTasks(bg, func(state Snapshot) error {
			if h.scheduler.invocations[id] != runtime || h.scheduler.runnable(state, state.Tasks[id]) {
				t.Error("sleep released actual permit before return")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		close(release)
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if _, err := h.WaitForTask(ctx, id); err != nil {
			t.Fatal(err)
		}
		if after.Load() != 1 {
			t.Fatal(after.Load())
		}
	})
}

func TestSleep140AutomaticTimerAndInvalidStates(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var rejected atomic.Int64
		def, err := DefineTask(TaskDefinitionOptions{Kind: "task.timer-sleep-140", Version: 1, Initial: func(any) (JSON, error) { return JSON{"phase": "start"}, nil }, Phases: map[string]TaskPhase{
			"start": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				for _, until := range []int64{-1, int64(MaxID) + 1} {
					if err := r.SleepUntil(ctx, until, JSON{"phase": "after"}); err == nil {
						return errors.New("invalid sleep accepted")
					}
					rejected.Add(1)
				}
				now, err := r.Now()
				if err != nil {
					return err
				}
				until := now + 25
				return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					id, err := tx.MintID()
					if err != nil {
						return nil, err
					}
					if err = tx.AppendEntry(Entry{ID: id, Conversation: r.ConversationID(), Kind: "sleep.marker", Value: JSON{"ok": true}}); err != nil {
						return nil, err
					}
					return &TaskState{Status: "sleeping", Until: &until, Checkpoint: JSON{"phase": "after"}}, nil
				})
			},
			"after": func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			},
		}, Abort: taskAbort})
		if err != nil {
			t.Fatal(err)
		}
		h := taskTestHarness(t, b.store, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		_, observer, err := h.session.SubscribeCommits(bg)
		if err != nil {
			t.Fatal(err)
		}
		defer observer.Stop()
		finished := make(chan struct{}, 1)
		if err = observer.Start(func(_ context.Context, frame PublicationFrame) error {
			for _, w := range frame.Publication.Tables {
				if w.Task != nil && w.Task.ID == id && w.Task.Status == "done" {
					select {
					case finished <- struct{}{}:
					default:
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		// No WaitForTask/Resume polling: only the foreground sleep timer wakes it.
		awaitTaskSignal(t, finished)
		if rejected.Load() != 2 {
			t.Fatal(rejected.Load())
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		var marker Entry
		for _, entry := range state.Entries {
			if entry.Kind == "sleep.marker" {
				marker = entry
			}
		}
		if marker.ID == 0 {
			t.Fatal("sleeping commit lost its atomic write")
		}
		for _, revision := range state.Tasks {
			if revision.Execution != nil && revision.Execution.Native != nil && revision.Execution.Native.State.Until != nil {
				t.Fatal("terminal task retained sleep deadline")
			}
		}
	})
}

func TestSleep140StateUnionAndWakeDelay(t *testing.T) {
	until := int64(0)
	if err := validateTaskState(TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "next"}, Until: &until}); err != nil {
		t.Fatal(err)
	}
	for _, state := range []TaskState{
		{Status: "sleeping", Checkpoint: JSON{"phase": "next"}},
		{Status: "running", Checkpoint: JSON{"phase": "next"}, Until: &until},
		{Status: "sleeping", Checkpoint: JSON{"phase": "next"}, Until: &until, On: []ID{1}, Policy: "allSettled"},
	} {
		if err := validateTaskState(state); err == nil {
			t.Fatal("invalid state accepted", state)
		}
	}
	if sleepWakeDelay(-1<<63, 1<<63-1) != time.Duration(1<<31-1)*time.Millisecond || sleepWakeDelay(2000, 1000) != 0 {
		t.Fatal("wake delay overflow")
	}
}
