package durable

import (
	"context"
	"time"
)

// Only the scheduler loop arms this single timer, and joins/disarms it at Close.
// The timer only requests a pass: the committed state, clock and definition fit
// are checked again on admission, including after early or stale fires.
func (s *taskScheduler) nextSleepDelay() (time.Duration, bool) {
	var earliest *int64
	err := s.h.session.readTasks(context.Background(), func(state Snapshot) error {
		var registry TaskRegistrySnapshot
		captured := false
		for _, task := range state.Tasks {
			if task.Execution == nil || task.Execution.Native == nil || task.Execution.Native.State.Status != "sleeping" || taskAborted(task) || s.invocations[task.ID] != nil {
				continue
			}
			if !captured {
				var err error
				registry, err = s.h.options.Registry.taskSnapshot(s.h.session.limits)
				if err != nil {
					return err
				}
				captured = true
			}
			if epoch, blocked := s.retryAfter[task.ID]; blocked && s.epoch.Load() <= epoch {
				continue
			}
			if s.nativeBlockedReason(task, registry) != "" {
				continue
			}
			until := *task.Execution.Native.State.Until
			if earliest == nil || until < *earliest {
				candidate := until
				earliest = &candidate
			}
		}
		return nil
	})
	if err != nil || earliest == nil {
		return 0, false
	}
	return sleepWakeDelay(s.h.session.now(), *earliest), true
}
func sleepWakeDelay(now, until int64) time.Duration {
	if until <= now {
		return 0
	}
	distance := uint64(until) - uint64(now)
	const maxDelay = uint64(1<<31 - 1)
	if distance > maxDelay {
		distance = maxDelay
	}
	return time.Duration(distance) * time.Millisecond
}
