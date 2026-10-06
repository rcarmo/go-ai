package durable

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestTaskGraphPinnedQueuedCancelledAcquisitionRegistersNothing(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		h := taskTestHarness(t, b.store)
		caller, cancel := context.WithCancel(bg)
		defer cancel()
		probe := &taskAdmissionContext{Context: caller, queued: make(chan struct{})}
		completed := make(chan error, 1)
		_, err := h.session.taskCommit(bg, func(*Tx) error {
			go func() {
				watch, err := h.WatchTaskGraph(probe)
				if watch != nil {
					watch.Stop()
					if err == nil {
						err = errors.New("cancelled graph acquired watch")
					}
				}
				completed <- err
			}()
			awaitTaskSignal(t, probe.queued)
			cancel()
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-completed:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("queued graph cancellation did not finish")
		}
		h.session.observerMu.Lock()
		count := len(h.session.subscriptions)
		h.session.observerMu.Unlock()
		if count != 0 {
			t.Fatal("cancelled acquisition retained subscription", count)
		}
		watch, err := h.WatchTaskGraph(bg)
		if err != nil {
			t.Fatal(err)
		}
		watch.Stop()
	})
}
