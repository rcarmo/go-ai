package durable

import (
	"context"
	"testing"
)

func TestClose140RequeueDoesNotConsumeRestartBudget(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered := make(chan struct{}, 8)
		def := taskDefinition(t, "task.clean-close-140", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			if _, err := r.MemoCandidate(ctx, "retained", "value"); err != nil {
				return err
			}
			entered <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		})
		limit := uint64(1)
		options := Options{Settings: &HarnessSettings{RestartLimit: &limit}}
		h := taskTestHarnessOptions(t, b.store, options, def)
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		for cycle := 0; cycle < 4; cycle++ {
			if err := h.Resume(bg); err != nil {
				t.Fatal(err)
			}
			awaitTaskSignal(t, entered)
			if err := h.Close(bg); err != nil {
				t.Fatal(err)
			}
			store := reopenStoreAfterHarnessClose(t, h.session.store)
			snapshot := snap(t, store)
			task := snapshot.Tasks[id]
			if task.Status != "pending" || task.Restarts != 0 || taskHasDecidedOutcome(task) || task.Execution.Native.State.Checkpoint["phase"] != "work" || task.Execution.Native.Memos["retained"] == nil {
				t.Fatal("clean shutdown changed execution/budget", task)
			}
			h = taskTestHarnessOptions(t, store, options, def)
			inspect, err := h.InspectTasks(bg)
			if err != nil || len(inspect.Tasks) != 1 || inspect.Tasks[0].Reason == "restart_limit" {
				t.Fatal(inspect, err)
			}
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}

func TestClose140RejectedRequeueLeavesCrashRecovery(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered := make(chan struct{})
		def := taskDefinition(t, "task.rejected-close-140", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		})
		registry := NewRegistry()
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		var original func(byte, uint64, uint64, []byte) error
		h.session.taskBookkeeping(func() {
			original = h.session.core.appendFrame
			h.session.core.appendFrame = func(byte, uint64, uint64, []byte) error { return reject("close write rejected") }
		})
		if err := h.Close(bg); err == nil {
			t.Fatal("lost close-time write failure")
		}
		h.session.core.appendFrame = original // Close has joined all host/storage work.
		store := reopenStoreAfterHarnessClose(t, h.session.store)
		task := snap(t, store).Tasks[id]
		if task.Status != "running" || task.Restarts != 0 || taskHasDecidedOutcome(task) {
			t.Fatal("rejected close write adopted", task)
		}
		h = taskTestHarness(t, store, def)
		snapshot, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if task := snapshot.Tasks[id]; task.Status != "pending" || task.Restarts != 1 {
			t.Fatal("crash recovery not retained", task)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
	})
}
