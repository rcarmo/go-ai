package durable

import (
	"context"
	"errors"
	"testing"
)

func TestTaskLifecyclePinnedCancelledCloseKeepsSharedShutdownAndStorageOwnership(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered, signalled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		storageRead := make(chan error, 1)
		var h *Harness
		definition := taskDefinition(t, "task.close-cancelled", func(ctx context.Context, _ TaskRecord, _ *TaskRuntime) error {
			close(entered)
			<-ctx.Done()
			close(signalled)
			<-release
			_, err := b.store.Snapshot(bg)
			storageRead <- err
			return nil
		})
		h = taskTestHarness(t, b.store, definition)
		cleanupTaskGates(t, release)
		id := createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		caller, cancel := context.WithCancel(bg)
		first := make(chan error, 1)
		go func() { first <- h.Close(caller) }()
		awaitTaskSignal(t, signalled)
		cancel()
		if err := <-first; !errors.Is(err, context.Canceled) {
			t.Fatal("caller did not cancel", err)
		}
		if _, err := h.CommitTasks(bg, 1, func(*Tx) error { t.Error("sealed callback ran"); return nil }); !errors.Is(err, ErrClosed) {
			t.Fatal("admission reopened", err)
		}
		if _, err := b.store.Snapshot(bg); err != nil {
			t.Fatal("cancelled close released storage", err)
		}
		root := &ConversationHandle{h: h, id: 1}
		checks := map[string]func() error{
			"conversation": func() error { _, err := h.Conversation(bg, 1); return err },
			"submission":   func() error { _, err := h.Submission(bg, ID(MaxID)); return err },
			"context":      func() error { _, err := root.Context(bg); return err },
			"context-view": func() error { _, err := root.ContextView(bg, 0); return err },
			"entries":      func() error { _, err := root.Entries(bg, EntryCursor{}, 1); return err },
		}
		for name, check := range checks {
			if err := check(); !errors.Is(err, ErrClosed) {
				t.Fatal("new read admitted after seal", name, err)
			}
		}
		// The second waiter owns the same closeDone and cannot finish before host return.
		second := make(chan error, 1)
		go func() { second <- h.Close(bg) }()
		select {
		case err := <-second:
			t.Fatal("second close skipped shared join", err)
		default:
		}
		releaseTaskGate(release)
		if err := <-storageRead; err != nil {
			t.Fatal("storage closed before host returned", err)
		}
		if err := <-second; err != nil {
			t.Fatal(err)
		}
		if _, err := b.store.Snapshot(bg); !errors.Is(err, ErrClosed) {
			t.Fatal("storage left open", err)
		}
		store := reopenStoreAfterHarnessClose(t, b.store)
		t.Cleanup(func() { _ = store.Close(bg) })
		state, err := store.Snapshot(bg)
		if err != nil || taskHasDecidedOutcome(state.Tasks[id]) {
			t.Fatal("shutdown invented outcome", state.Tasks[id], err)
		}
	})
}
