package durable

import (
	"context"
	"fmt"
	"sync/atomic"
)

// Wake describes work that needs a running process, even while paused. At is
// the earliest future sleep on the Harness epoch-millisecond clock. Active
// includes actual hosts and unfinished reconciliation, not just runnable phases.
type Wake struct {
	Active bool   `json:"active"`
	At     *int64 `json:"at,omitempty"`
}

func copyWake(w Wake) Wake {
	if w.At != nil {
		at := *w.At
		w.At = &at
	}
	return w
}
func sameWake(a, b Wake) bool {
	return a.Active == b.Active && (a.At == nil && b.At == nil || a.At != nil && b.At != nil && *a.At == *b.At)
}
func earlier(at **int64, candidate int64) {
	if *at == nil || candidate < **at {
		copy := candidate
		*at = &copy
	}
}

type wakeDetails struct {
	wake    Wake
	alarmAt *int64
}

// Wake is an observational Session-line read. It never dispatches code, invokes
// migrations, changes task state or enables scheduling.
func (h *Harness) Wake(ctx context.Context) (Wake, error) {
	details, err := h.readWake(ctx)
	return details.wake, err
}
func (h *Harness) readWake(ctx context.Context) (wakeDetails, error) {
	var details wakeDetails
	err := h.session.readTasks(ctx, func(state Snapshot) error {
		if h.closing.Load() {
			return ErrClosed
		}
		s := h.scheduler
		details.wake.Active = len(s.invocations) > 0
		var registry TaskRegistrySnapshot
		captured := false
		var now int64
		clockRead := false
		fit := func(task Task) (bool, error) {
			if taskAborted(task) && task.AbortReason != "restart" {
				return true, nil
			}
			if s.parked(task) {
				return false, nil
			}
			if task.Execution == nil || task.Execution.Native == nil {
				return taskManaged(task), nil
			}
			if !captured {
				var err error
				registry, err = h.options.Registry.taskSnapshot(h.session.limits)
				if err != nil {
					return false, err
				}
				captured = true
			}
			definition := registry.Task(task.Kind)
			return definition != nil && definition.Version() >= task.Execution.Native.Version && s.failed[task.ID] != definition, nil
		}
		for _, task := range state.Tasks {
			if terminalStatus(task.Status) {
				continue
			}
			if s.abandoned[task.ID] || taskBelowCancelled(state, task) && (!taskAborted(task) || task.AbortReason == "restart" && restartCascadeReason(state, task) == "") || !taskAborted(task) && taskSelectedFailFastCancellation(state, task.ID) {
				details.wake.Active = true
			}
			if taskHasDecidedOutcome(task) {
				if len(s.ownedLive(state, task.ID)) == 0 && s.invocations[task.ID] == nil {
					details.wake.Active = true
				}
				continue
			}
			until, sleeping := taskSleepDeadline(task)
			if sleeping && !taskAborted(task) {
				if !clockRead {
					now = h.session.now()
					clockRead = true
				}
				if until > now {
					earlier(&details.wake.At, until)
					earlier(&details.alarmAt, until)
					continue
				}
			}
			if s.heldFailedInput(state, task) {
				continue
			}
			eligible, err := fit(task)
			if err != nil {
				return err
			}
			if !eligible {
				continue
			}
			if s.runnable(state, task) {
				details.wake.Active = true
				// A clock can cross the deadline between reserve and this read. Request
				// another pass without advancing any retry barrier or fabricating a write.
				if sleeping && !taskAborted(task) && len(s.invocations) < s.capacity() {
					earlier(&details.alarmAt, until)
				}
			}
		}
		return nil
	})
	return details, err
}

type sleepAlarm struct {
	at             int64
	fired, stopped atomic.Bool
	cancel         func()
}
type wakePublisher struct {
	last  *Wake
	alarm *sleepAlarm
}

func (p *wakePublisher) stopAlarm() {
	if p.alarm != nil {
		alarm := p.alarm
		p.alarm = nil
		alarm.stopped.Store(true)
		if alarm.cancel != nil {
			alarm.cancel()
		}
	}
}

// Owned by the existing single scheduler loop. No host function runs while the
// Session line is held, and delivery/alarms are coalesced by their semantic value.
func (p *wakePublisher) update(s *taskScheduler) {
	details, err := s.h.readWake(context.Background())
	if err != nil {
		p.stopAlarm()
		if !s.h.closing.Load() {
			s.report(err)
		}
		return
	}
	if s.h.options.OnWake != nil {
		p.stopAlarm()
		if p.last != nil && sameWake(*p.last, details.wake) {
			return
		}
		offered := copyWake(details.wake)
		delivered := false
		func() {
			defer func() {
				if value := recover(); value != nil {
					s.h.report(context.Background(), fmt.Errorf("OnWake callback panic: %v", value), ReportInfo{Type: "host_error", Callback: "onWake"})
				}
			}()
			s.h.options.OnWake(copyWake(offered))
			delivered = true
		}()
		if delivered {
			p.last = &offered
		}
		return
	}
	if !s.enabled.Load() || s.sealed.Load() || details.alarmAt == nil {
		p.stopAlarm()
		return
	}
	at := *details.alarmAt
	if p.alarm != nil && p.alarm.at == at && !p.alarm.fired.Load() {
		return
	}
	p.stopAlarm()
	alarm := &sleepAlarm{at: at}
	p.alarm = alarm
	alarm.cancel = s.h.armTimer(func() {
		if !alarm.stopped.Load() {
			alarm.fired.Store(true)
			s.kick()
		}
	}, sleepWakeDelay(s.h.session.now(), at), TimerOptions{})
}
