package durable

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Assertion crosswalk: upstream 81f3cd41 harness-idle-scopes.test.ts wake table.
// IDs, native envelopes and phase names differ; wake values and owner/wait
// relationships are retained. These checks do not prove indexed work bounds.
func mainWakeSourceTask(id ID, state TaskState) Task {
	task := foundationTask(id, nil)
	task.Kind = "task.wake-source"
	task.Status = state.Status
	task.Execution.Native.State = state
	return task
}

func mainWakeSourceSleep(id ID, until int64) Task {
	return mainWakeSourceTask(id, TaskState{Status: "sleeping", Checkpoint: JSON{"phase": "work"}, Until: &until})
}

func mainWakeSourceSeed(t *testing.T, store Storage, tasks ...Task) {
	t.Helper()
	writes := make([]Write, len(tasks))
	for i := range tasks {
		writes[i] = Write{Op: "put-task", Task: &tasks[i]}
	}
	if _, err := store.Apply(bg, Batch{Writes: writes}); err != nil {
		t.Fatal(err)
	}
}

func TestMainWakeSourceTable(t *testing.T) {
	cases := []struct {
		name   string
		build  func(ID, ID) []Task
		active bool
		at     int64
	}{
		{"sleeping", func(a, b ID) []Task { return []Task{mainWakeSourceSleep(a, 5000)} }, false, 5000},
		{"abort-marked-sleeping", func(a, b ID) []Task {
			task := mainWakeSourceSleep(b, 3000)
			task.Execution.Native.AbortRequested = true
			return []Task{mainWakeSourceSleep(a, 5000), task}
		}, true, 5000},
		{"due-sleep", func(a, b ID) []Task { return []Task{mainWakeSourceSleep(a, 500)} }, true, 0},
		{"waiting-on-live-work", func(a, b ID) []Task {
			return []Task{mainWakeSourceSleep(a, 5000), mainWakeSourceTask(b, TaskState{Status: "waiting", Checkpoint: JSON{"phase": "work"}, On: []ID{a}, Policy: "allSettled"})}
		}, false, 5000},
		{"completing-owner", func(a, b ID) []Task {
			child := mainWakeSourceSleep(a, 5000)
			child.Owner = b
			parent := mainWakeSourceTask(b, TaskState{Status: "completing", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: nil}}})
			return []Task{parent, child}
		}, false, 5000},
		{"missing-definition", func(a, b ID) []Task {
			task := mainWakeSourceTask(b, TaskState{Status: "pending", Checkpoint: JSON{"phase": "work"}})
			task.Kind = "task.wake-source-missing"
			return []Task{mainWakeSourceSleep(a, 5000), task}
		}, false, 5000},
		{"pending-work", func(a, b ID) []Task {
			return []Task{mainWakeSourceSleep(a, 5000), mainWakeSourceTask(b, TaskState{Status: "pending", Checkpoint: JSON{"phase": "work"}})}
		}, true, 5000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			utf16Backends(t, func(t *testing.T, b backend) {
				a, err := b.store.MintID(bg)
				if err != nil {
					t.Fatal(err)
				}
				second, err := b.store.MintID(bg)
				if err != nil {
					t.Fatal(err)
				}
				mainWakeSourceSeed(t, b.store, c.build(a, second)...)
				def := taskDefinition(t, "task.wake-source", func(context.Context, TaskRecord, *TaskRuntime) error {
					t.Error("observational Wake invoked a host")
					return nil
				})
				h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 { return 1000 }}, def)
				before, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				wake, err := h.Wake(bg)
				want := Wake{Active: c.active}
				if c.at != 0 {
					want.At = &c.at
				}
				if err != nil || !sameWake(wake, want) {
					t.Fatal(wake, want, err)
				}
				after, err := h.Snapshot(bg)
				if err != nil || after.Seq != before.Seq {
					t.Fatal("Wake wrote state", before.Seq, after.Seq, err)
				}
			})
		})
	}
}

func TestMainWakeSourceRunningInvocationRetainsEarliestSleep(t *testing.T) {
	utf16Backends(t, func(t *testing.T, b backend) {
		a, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		id, err := b.store.MintID(bg)
		if err != nil {
			t.Fatal(err)
		}
		mainWakeSourceSeed(t, b.store, mainWakeSourceSleep(a, 5000), mainWakeSourceTask(id, TaskState{Status: "pending", Checkpoint: JSON{"phase": "work"}}))
		started, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		unblock := func() { once.Do(func() { close(release) }) }
		defer unblock()
		def := taskDefinition(t, "task.wake-source", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		events := make(chan Wake, 128)
		h := taskTestHarnessOptions(t, b.store, Options{Now: func() int64 { return 1000 }, OnWake: func(w Wake) { events <- copyWake(w) }}, def)
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("real invocation did not start")
		}
		at := int64(5000)
		if wake, err := h.Wake(bg); err != nil || !sameWake(wake, Wake{Active: true, At: &at}) {
			t.Fatal(wake, err)
		}
		unblock()
		ctx, cancel := context.WithTimeout(bg, 5*time.Second)
		defer cancel()
		if _, err := h.WaitForTask(ctx, id); err != nil {
			t.Fatal(err)
		}
		// Outcome publication can precede actual host retirement. Wait for its
		// observed wake transition; WaitForIdle also waits for the future sleeper.
		awaitWake140(t, events, false, &at)
		if wake, err := h.Wake(bg); err != nil || !sameWake(wake, Wake{At: &at}) {
			t.Fatal(wake, err)
		}
	})
}
