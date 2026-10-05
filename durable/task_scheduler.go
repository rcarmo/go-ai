package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"sort"
	"sync"
	"sync/atomic"
)

// taskScheduler is the sole task reservation/cancellation/join owner. All maps
// below belong to the Session line; off-line code only signals wake/seal flags.
type taskScheduler struct {
	h              *Harness
	enabled        atomic.Bool
	sealed         atomic.Bool
	wake           chan struct{}
	done           chan struct{}
	invocations    map[ID]*TaskRuntime
	failed         map[ID]*TaskDefinition
	failures       map[ID]error
	retryAfter     map[ID]uint64 // failed host admission requires a newer external epoch
	epoch          atomic.Uint64
	reconcileRetry *uint64 // failed pass ignores self/stale wake tokens
	cursor         ID
	reports        []error                   // line decision failures; delivered after leave
	tickets        map[ID]map[*taskWait]bool // target -> individual wait registrations
	waiters        map[*taskWait]bool
	unsubscribe    func()
	stopMu         sync.Mutex
	stops          []taskStop
}

func newTaskScheduler(h *Harness) *taskScheduler {
	return &taskScheduler{h: h, wake: make(chan struct{}, 1), done: make(chan struct{}), invocations: map[ID]*TaskRuntime{}, failed: map[ID]*TaskDefinition{}, failures: map[ID]error{}, retryAfter: map[ID]uint64{}, tickets: map[ID]map[*taskWait]bool{}, waiters: map[*taskWait]bool{}}
}
func (s *taskScheduler) kick() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *taskScheduler) externalKick() { s.epoch.Add(1); s.kick() }
func (s *taskScheduler) enable()       { s.enabled.Store(true); s.externalKick() }
func (s *taskScheduler) capacity() int {
	n := s.h.session.limits.MaxPage
	if n > 64 {
		n = 64
	}
	return n
}

// Reporting is synchronous and host-only, always outside Session/storage lines.
// A callback panic cannot replace the original operation's error.
func reportTaskError(report func(error), err error) {
	if err != nil && report != nil {
		func() { defer func() { _ = recover() }(); report(err) }()
	}
}
func (s *taskScheduler) report(err error) { reportTaskError(s.h.options.OnReport, err) }
func (s *taskScheduler) loop() {
	defer close(s.done)
	for {
		select {
		case <-s.h.life.Done():
			return
		case <-s.wake:
		}
		if s.sealed.Load() || !s.enabled.Load() {
			continue
		}
		if err := s.reconcilePass(true); err != nil {
			if !errors.Is(err, errTaskPassAwaitingWake) {
				s.report(err)
			}
			continue
		}
		reservations, err := s.reserve()
		if err != nil {
			s.report(err)
			continue
		}
		for _, runtime := range reservations {
			go s.run(runtime)
		}
		s.updateDiagnostics()
	}
}
func (s *taskScheduler) updateDiagnostics() {
	busy := map[ID]bool{}
	retiredPins := map[ID]bool{}
	err := s.h.session.readTasks(context.Background(), func(state Snapshot) error {
		for _, task := range state.Tasks {
			if task.Kind == "pi.generation" {
				if !terminalStatus(task.Status) {
					busy[task.Conversation] = true
				}
				if terminalStatus(task.Status) && s.invocations[task.ID] == nil {
					retiredPins[task.ID] = true
				}
			}
		}
		return nil
	})
	if err != nil {
		return
	}
	s.h.mu.Lock()
	s.h.workers = busy
	// Delete only witnessed immutable terminals. A new pin installed after
	// this line snapshot must not be pruned merely because it was absent there.
	for id := range retiredPins {
		delete(s.h.pins, id)
	}
	close(s.h.changed)
	s.h.changed = make(chan struct{})
	s.h.mu.Unlock()
}

func taskManaged(task Task) bool {
	return task.Kind == "pi.generation" || task.Kind == "pi.tool" || task.Execution != nil && task.Execution.Native != nil
}
func (s *taskScheduler) runnable(state Snapshot, task Task) bool {
	if !taskManaged(task) {
		return false
	}
	if epoch, blocked := s.retryAfter[task.ID]; blocked && s.epoch.Load() <= epoch {
		return false
	}
	if terminalStatus(task.Status) || taskHasDecidedOutcome(task) {
		return false
	}
	// Bounded mark prefixes cannot admit a later unmarked descendant/sibling
	// ordinary phase while its already-confirmed cascade intent is unresolved.
	if !taskAborted(task) && (taskBelowCancelled(state, task) || taskSelectedFailFastCancellation(state, task.ID)) {
		return false
	}
	if _, active := s.invocations[task.ID]; active {
		return false
	}
	if taskAborted(task) && task.Kind != "pi.tool" {
		return len(s.ownedLive(state, task.ID)) == 0
	}
	if task.Execution != nil && task.Execution.Native != nil && task.Execution.Native.State.Status == "waiting" {
		for _, id := range task.Execution.Native.State.On {
			if t, ok := state.Tasks[id]; !ok || !terminalStatus(t.Status) {
				return false
			}
		}
	}
	if task.Kind == "pi.generation" {
		var cp generationCheckpoint
		if fromObject(task.Checkpoint, &cp, s.h.session.limits) != nil {
			return false
		}
		// A conversation queue remains ID-ordered, independent of worker launch.
		for _, other := range state.Tasks {
			if other.Kind == task.Kind && other.Conversation == task.Conversation && other.ID < task.ID && !terminalStatus(other.Status) {
				return false
			}
		}
		if cp.Phase == "compaction" {
			child, exists := state.Tasks[cp.Compaction]
			return exists && terminalStatus(child.Status) && s.invocations[child.ID] == nil && len(s.ownedLive(state, task.ID)) == 0
		}
		if cp.Phase == "tools" {
			children, err := s.toolRoundChildren(state, task, cp)
			if err != nil {
				return false
			}
			for _, child := range children {
				if !terminalStatus(child.Status) || s.invocations[child.ID] != nil {
					return false
				}
			}
			return len(s.ownedLive(state, task.ID)) == 0
		}
		return true
	}
	if task.Kind == "pi.tool" {
		parent, ok := state.Tasks[task.Owner]
		if !ok || parent.Kind != "pi.generation" {
			return false
		}
		var cp generationCheckpoint
		if fromObject(parent.Checkpoint, &cp, s.h.session.limits) != nil {
			return false
		}
		children, err := s.toolRoundChildren(state, parent, cp)
		if err != nil {
			return false
		}
		if !cp.Sequential && cp.ToolExecution == "parallel" {
			for _, child := range children {
				if child.ID == task.ID {
					return !taskAborted(task) || len(s.ownedLive(state, task.ID)) == 0
				}
			}
			return false
		}
		for _, child := range children {
			if terminalStatus(child.Status) && s.invocations[child.ID] == nil {
				continue
			}
			return child.ID == task.ID && (!taskAborted(task) || len(s.ownedLive(state, task.ID)) == 0)
		}
		return false
	}
	return task.Execution != nil && task.Execution.Native != nil
}
func (s *taskScheduler) reserve() ([]*TaskRuntime, error) { return s.reservePass(true) }
func (s *taskScheduler) reservePass(requireEnabled bool) ([]*TaskRuntime, error) {
	reservations := []*TaskRuntime{}
	attempted := map[ID]bool{}
	var attemptEpoch uint64
	_, err := s.h.session.taskCommit(context.Background(), func(tx *Tx) error {
		attemptEpoch = s.epoch.Load()
		tx.taskRollback = func(failure error) {
			// This callback belongs to Session's original admission. No wait
			// can observe a rejected provisional runtime before its removal.
			for id := range attempted {
				s.retryAfter[id] = attemptEpoch
			}
			for _, r := range reservations {
				if s.invocations[r.taskID] == r {
					delete(s.invocations, r.taskID)
				}
				for wait := range s.waiters {
					if wait.claims[r.taskID] || wait.target == r.taskID {
						s.signalWait(wait, failure)
					}
				}
				r.ended.Store(true)
				close(r.done)
			}
			// Revalidate ALL surviving dependency registrations against the
			// unchanged confirmed state, including A -> held P -> pending B.
			if !errors.Is(failure, ErrPoisoned) {
				s.refreshBoundWaits(tx.state)
			}
		}
		if s.sealed.Load() || s.h.closing.Load() || requireEnabled && !s.enabled.Load() {
			return nil
		}
		snapshot, err := s.h.options.Registry.taskSnapshot(tx.limits)
		if err != nil {
			return err
		}
		for target := range s.tickets {
			task, ok := tx.state.Tasks[target]
			if !ok || !s.runnable(tx.state, task) {
				s.failTicket(target, &InvocationWaitRequiresYield{Task: target})
			}
		}
		order := ids(tx.state.Tasks)
		sort.SliceStable(order, func(i, j int) bool {
			a, b := order[i], order[j]
			return (a > s.cursor) != (b > s.cursor) && a > s.cursor || (a > s.cursor) == (b > s.cursor) && a < b
		})
		tryReserve := func(id ID, ticketed bool) (bool, error) {
			// One task transition per admission: later candidates cannot reject
			// an earlier valid prefix under persisted write/frame budgets. Its
			// adoption wakes the next pass, filling the unchanged capacity C.
			if len(tx.writes) > 0 || len(s.invocations) >= s.capacity() {
				return true, nil
			}
			task := tx.state.Tasks[id]
			if !s.runnable(tx.state, task) {
				return false, nil
			}
			if (s.tickets[id] != nil) != ticketed {
				return false, nil
			}
			if !ticketed && len(s.invocations)+len(s.tickets) >= s.capacity() {
				return false, nil
			}
			attempted[id] = true
			var definition *TaskDefinition
			if task.Kind == "pi.generation" {
				var cp generationCheckpoint
				if err := fromObject(task.Checkpoint, &cp, tx.limits); err != nil {
					return false, err
				}
				if cp.Phase != "queued" && cp.Phase != "prepare-next" && cp.Phase != "intent" && cp.Phase != "tools" && cp.Phase != "retry" && cp.Phase != "compaction" && cp.Phase != "poll" {
					s.failTicket(id, &InvocationWaitRequiresYield{Task: id})
					if err := s.builtinDecision(tx, task, TaskOutcome{Status: "orphaned", Reason: "missing_builtin_phase"}); err != nil {
						return false, err
					}
					return len(tx.writes) > 0, nil
				}
			}
			if task.Execution != nil && task.Execution.Native != nil {
				definition = snapshot.Task(task.Kind)
				if definition == nil || definition.Version() < task.Execution.Native.Version {
					s.failTicket(id, &InvocationWaitRequiresYield{Task: id})
					if taskAborted(task) {
						reason := "missing_task"
						if definition != nil {
							reason = "task_too_old"
						}
						if err := s.orphan(tx, task, reason); err != nil {
							return false, err
						}
					}
					return len(tx.writes) > 0, nil
				}
				if s.failed[id] == definition {
					s.failTicket(id, &InvocationWaitRequiresYield{Task: id})
					if taskAborted(task) {
						if err := s.orphan(tx, task, "migration_failed"); err != nil {
							return false, err
						}
					}
					return len(tx.writes) > 0, nil
				}
				if definition.Version() > task.Execution.Native.Version {
					if definition.options.Migrate == nil {
						s.failed[id] = definition
						s.failures[id] = reject("missing task migration")
						s.reports = append(s.reports, s.failures[id])
						s.failTicket(id, &InvocationWaitRequiresYield{Task: id})
						if taskAborted(task) {
							if err := s.orphan(tx, task, "migration_failed"); err != nil {
								return false, err
							}
						}
						return len(tx.writes) > 0, nil
					}
					input, err := copyTaskValue(task.Execution.Native.Input, tx.limits)
					if err != nil {
						return false, err
					}
					checkpoint, err := copyObject(task.Execution.Native.State.Checkpoint, tx.limits)
					if err != nil {
						return false, err
					}
					nextInput, nextCheckpoint, err := callTaskMigration(definition, input.Value, checkpoint, task.Execution.Native.Version)
					if err == nil {
						trial, e := copyTask(task, tx.limits)
						if e != nil {
							err = e
						} else {
							trial.Execution.Native.Input = &TaskValue{Present: true, Value: nextInput}
							trial.Execution.Native.State.Checkpoint = nextCheckpoint
							trial.Execution.Native.Version = definition.Version()
							_, err = copyTask(trial, tx.limits)
							if err == nil {
								phase, _ := nextCheckpoint["phase"].(string)
								if definition.options.Phases[phase] == nil {
									err = reject("migrated phase unavailable")
								}
							}
						}
					}
					if err != nil {
						s.failed[id] = definition
						s.failures[id] = err
						s.reports = append(s.reports, err)
						s.failTicket(id, &InvocationWaitRequiresYield{Task: id})
						if taskAborted(task) {
							if err := s.orphan(tx, task, "migration_failed"); err != nil {
								return false, err
							}
						}
						return len(tx.writes) > 0, nil
					}
					native := *task.Execution.Native
					task.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &native}
					native.Input = &TaskValue{Present: true, Value: nextInput}
					native.State.Checkpoint = nextCheckpoint
					native.Version = definition.Version()
				}
			}
			owned, err := copyTask(task, tx.limits)
			if err != nil {
				return false, err
			}
			task = owned
			task.Status = "running"
			if task.Execution != nil && task.Execution.Native != nil {
				task.Execution.Native.State.Status = "running"
				task.Execution.Native.State.On = nil
				task.Execution.Native.State.Policy = ""
			}
			if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
				return false, err
			}
			ctx, cancel := context.WithCancel(s.h.life)
			runtime := &TaskRuntime{harness: s.h, taskID: id, conversation: task.Conversation, context: ctx, cancel: cancel, registry: snapshot, definition: definition, abortMode: taskAborted(task), done: make(chan struct{})}
			delete(s.tickets, id)
			s.invocations[id] = runtime
			reservations = append(reservations, runtime)
			s.cursor = id
			return true, nil
		}
		for _, ticketed := range []bool{true, false} {
			for _, id := range order {
				stop, err := tryReserve(id, ticketed)
				if err != nil {
					return err
				}
				if stop {
					return nil
				}
			}
		}
		return nil
	})
	var reports []error
	s.h.session.taskBookkeeping(func() { reports = s.reports; s.reports = nil })
	if err != nil {
		// Rollback already ran before the Session line was released. Only host
		// cancellation/report delivery happens here, off-line.
		for _, r := range reservations {
			r.cancel()
		}
		for _, failure := range reports {
			s.report(failure)
		}
		return nil, err
	}
	for _, failure := range reports {
		s.report(failure)
	}
	return reservations, nil
}
func (s *taskScheduler) runBuiltin(runtime *TaskRuntime) {
	var task Task
	err := s.h.session.readTasks(context.Background(), func(state Snapshot) error {
		if s.sealed.Load() {
			return ErrClosed
		}
		current, ok := state.Tasks[runtime.taskID]
		if !ok {
			return ErrSealed
		}
		if !runtime.abortMode && (taskAborted(current) || taskBelowCancelled(state, current) || taskSelectedFailFastCancellation(state, current.ID)) {
			s.endOnLine(runtime)
			return ErrSealed
		}
		var err error
		task, err = copyTask(current, s.h.session.limits)
		return err
	})
	if err != nil {
		return
	}
	if task.Kind == "pi.tool" {
		if err := s.h.executeTool(task, runtime); err != nil {
			runtime.admissionFailed = true
			s.report(err)
			return
		}
	}
	if task.Kind == "pi.generation" {
		var cp generationCheckpoint
		if err := fromObject(task.Checkpoint, &cp, s.h.session.limits); err != nil {
			s.report(err)
			return
		}
		if err := s.h.runGenerationInvocation(runtime, task, cp); err != nil {
			runtime.admissionFailed = true
			s.report(err)
			return
		}
	}
	// A successful built-in unit must either decide an outcome or make durable
	// progress. Uncaught corruption/no-progress is a SCHEDULER fault, not an
	// ordinary unavailable/interrupted tool error receipt.
	_, err = s.h.session.invocationCommit(context.Background(), runtime.taskID, func(tx *Tx) error {
		current, ok := tx.state.Tasks[runtime.taskID]
		if !ok || current.Status != "running" || taskHasDecidedOutcome(current) || taskAborted(current) || s.sealed.Load() || s.h.closing.Load() {
			return nil
		}
		before, e := ownJSONValue(task.Checkpoint, tx.limits)
		if e != nil {
			return e
		}
		after, e := ownJSONValue(current.Checkpoint, tx.limits)
		if e != nil {
			return e
		}
		if !equalDeltaJSON(before, after) {
			return nil
		}
		return s.builtinDecision(tx, current, TaskOutcome{Status: "faulted", Error: &TaskOutcomeError{Message: "builtin_no_progress"}})
	})
	if err != nil {
		runtime.admissionFailed = true
		s.report(err)
	}
}

func (s *taskScheduler) run(runtime *TaskRuntime) {
	defer func() {
		var watches []*DocumentWatch
		s.h.session.taskBookkeeping(func() { runtime.ended.Store(true); watches = runtime.watches; runtime.watches = nil })
		// Stop invocation-owned watches before cancelling their acquisition
		// context, so normal host return has a deterministic stopped reason.
		for _, watch := range watches {
			watch.Stop()
		}
		runtime.cancel()
		s.h.session.taskBookkeeping(func() {
			if s.invocations[runtime.taskID] == runtime {
				delete(s.invocations, runtime.taskID)
			}
			if runtime.admissionFailed {
				s.retryAfter[runtime.taskID] = runtime.admissionEpoch
			} else {
				delete(s.retryAfter, runtime.taskID)
				s.epoch.Add(1) // genuine successful host return frees dependency work
			}
			// Release and frontier re-ticketing share ONE line admission. Cleanup
			// precedes the optional read: poison never loses the actual host join.
			state, err := s.h.session.taskSnapshot(context.Background())
			if err == nil && !s.sealed.Load() {
				s.refreshBoundWaits(state)
			}
			s.notifyWaiters(err)
			// Close observes both the actual return AND completed line bookkeeping.
			close(runtime.done)
		})
		s.h.notify()
		if !s.h.closing.Load() {
			s.kick()
		}
	}()
	if s.sealed.Load() || s.h.closing.Load() {
		return
	}
	if runtime.definition == nil {
		s.runBuiltin(runtime)
		return
	}
	var previous JSON
	var handlerError error
	for {
		var task TaskRecord
		var proceed bool
		var handoverReport error
		_, err := s.h.session.invocationCommit(context.Background(), runtime.taskID, func(tx *Tx) error {
			raw, ok := tx.state.Tasks[runtime.taskID]
			if !ok || s.sealed.Load() || runtime.ended.Load() || raw.Status != "running" {
				s.endOnLine(runtime)
				return nil
			}
			view, err := CanonicalTask(raw, tx.limits)
			if err != nil {
				return err
			}
			task = view
			if !runtime.abortMode && (view.AbortRequested || taskBelowCancelled(tx.state, raw) || taskSelectedFailFastCancellation(tx.state, raw.ID)) {
				// Confirmed cascade intent wins at every phase boundary, even
				// before this task's bounded mark prefix is adopted. End/join;
				// reconcile commits the mark before a fresh abort reservation.
				s.endOnLine(runtime)
				return nil
			}
			if previous != nil {
				if handlerError != nil {
					return s.fault(tx, raw, handlerError)
				}
				if runtime.abortMode {
					return s.fault(tx, raw, reject("abort handler returned without outcome"))
				}
				before, err := ownJSONValue(previous, tx.limits)
				if err != nil {
					return err
				}
				after, err := ownJSONValue(view.State.Checkpoint, tx.limits)
				if err != nil {
					return err
				}
				if equalDeltaJSON(before, after) {
					return s.fault(tx, raw, reject("task phase returned without durable progress"))
				}
				snapshot, err := s.h.options.Registry.taskSnapshot(tx.limits)
				if err != nil {
					return err
				}
				next := snapshot.Task(raw.Kind)
				if next != runtime.definition && next != nil && (next.Version() == view.Version || next.Version() > view.Version && next.options.Migrate != nil) {
					owned, err := copyTask(raw, tx.limits)
					if err != nil {
						return err
					}
					raw = owned
					raw.Status = "pending"
					raw.Execution.Native.State.Status = "pending"
					tx.taskStops = append(tx.taskStops, taskStop{runtime: runtime})
					return tx.stage(Write{Op: "put-task", Task: &raw})
				}
				if next != runtime.definition && (!runtime.handoverReported || runtime.handoverToken != next) {
					code := "incompatible_task"
					if next == nil {
						code = "missing_task"
					}
					handoverReport = reject(code)
					runtime.handoverReported = true
					runtime.handoverToken = next
				} else if next == runtime.definition {
					runtime.handoverReported = false
					runtime.handoverToken = nil
				}
				runtime.phaseMu.Lock()
				runtime.registry = snapshot
				runtime.phaseMu.Unlock()
			}
			proceed = true
			return nil
		})
		// Reports never run under Session/Registry locks. One retained resolved
		// token suppresses repeated boundaries without retaining a token history.
		if handoverReport != nil {
			s.report(handoverReport)
		}
		if err != nil {
			runtime.admissionFailed = true
			s.report(err)
			return
		}
		if !proceed || s.sealed.Load() || s.h.closing.Load() || runtime.ended.Load() {
			return
		}
		previous, _ = copyObject(task.State.Checkpoint, s.h.session.limits)
		handler := runtime.definition.options.Phases[task.State.Checkpoint["phase"].(string)]
		if runtime.abortMode {
			handler = runtime.definition.options.Abort
		}
		if handler == nil {
			handlerError = reject("task phase unavailable")
		} else {
			handlerError = callTaskPhase(handler, runtime.context, task, runtime)
		}
	}
}
func callTaskMigration(def *TaskDefinition, input any, checkpoint JSON, version uint64) (value any, next JSON, err error) {
	defer func() {
		if recover() != nil {
			err = reject("task migration panic")
		}
	}()
	return def.options.Migrate(input, checkpoint, version)
}

func callTaskPhase(handler TaskPhase, ctx context.Context, task TaskRecord, runtime *TaskRuntime) (err error) {
	defer func() {
		if recover() != nil {
			err = reject("task handler panic")
		}
	}()
	return handler(ctx, task, runtime)
}

// nativeBlockedReason is a pure fit of an already captured definition snapshot.
// Unknown migration results remain reservation work; no host code runs here.
func (s *taskScheduler) nativeBlockedReason(task Task, snapshot TaskRegistrySnapshot) string {
	if task.Execution == nil || task.Execution.Native == nil {
		return ""
	}
	definition := snapshot.Task(task.Kind)
	if definition == nil {
		return "missing_task"
	}
	version := task.Execution.Native.Version
	if definition.Version() < version {
		return "task_too_old"
	}
	if s.failed[task.ID] == definition || definition.Version() > version && definition.options.Migrate == nil {
		return "migration_failed"
	}
	return ""
}

func (s *taskScheduler) orphan(tx *Tx, task Task, reason string) error {
	owned, err := copyTask(task, tx.limits)
	if err != nil {
		return err
	}
	task = owned
	outcome := TaskOutcome{Status: "orphaned", Reason: reason}
	if task.Execution == nil || task.Execution.Native == nil {
		return reject("orphan generic task envelope missing")
	}
	task.Execution.Native.State = TaskState{Status: "terminal", Outcome: &outcome}
	task.Execution.Native.Memos = nil
	task.Status = "failed"
	return tx.stage(Write{Op: "put-task", Task: &task})
}

func (s *taskScheduler) fault(tx *Tx, task Task, err error) error {
	outcome := TaskOutcome{Status: "faulted", Error: &TaskOutcomeError{Message: "task_phase_fault"}}
	if errors.Is(err, ErrClosed) {
		return nil
	}
	owned, e := copyTask(task, tx.limits)
	if e != nil {
		return e
	}
	task = owned
	native := *task.Execution.Native
	native.State = TaskState{Status: "terminal", Outcome: &outcome}
	native.Memos = nil
	task.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &native}
	task.Status = "failed"
	candidate := tx.state
	if len(tx.writes) > 0 {
		candidate, e = tx.current()
		if e != nil {
			return e
		}
	}
	if len(s.ownedLive(candidate, task.ID)) > 0 {
		task.Status = "completing"
		native.State.Status = "completing"
	}
	runtime := s.invocations[task.ID]
	if runtime != nil {
		tx.taskStops = append(tx.taskStops, taskStop{runtime: runtime})
	}
	return tx.stage(Write{Op: "put-task", Task: &task})
}
func taskAborted(task Task) bool {
	if task.Execution != nil {
		if task.Execution.Native != nil && task.Execution.Native.AbortRequested {
			return true
		}
		if task.Execution.Builtin != nil && task.Execution.Builtin.AbortRequested {
			return true
		}
	}
	aborted, _ := task.Checkpoint["abort"].(bool)
	return aborted
}
func taskOwnedLive(state Snapshot, owner ID) []ID {
	result := []ID{}
	for _, id := range ids(state.Tasks) {
		task := state.Tasks[id]
		if terminalStatus(task.Status) || taskBackground(task) {
			continue
		}
		current := task
		seen := map[ID]bool{}
		for {
			parentID := current.Owner
			if parentID == 0 {
				parentID = state.Conversations[current.Conversation].Owner
			}
			if parentID == 0 || seen[parentID] {
				break
			}
			seen[parentID] = true
			if parentID == owner {
				result = append(result, id)
				break
			}
			parent, ok := state.Tasks[parentID]
			if !ok || taskBackground(parent) {
				break
			}
			current = parent
		}
	}
	return result
}
func taskBelowCancelled(state Snapshot, task Task) bool {
	if taskBackground(task) {
		return false
	}
	// Also derive selected-ancestor intent for conversation-only probes used
	// by queued input withdrawal, before bounded marks reach their owners.
	if taskInheritedSelectedFailFastCancellation(state, task) {
		return true
	}
	owners, err := taskOwnerChain(state, task)
	if err != nil {
		return false
	}
	// Walk in order so an unmarked background owner stops ordinary traversal.
	current := task
	seen := map[ID]bool{}
	for {
		id := current.Owner
		if id == 0 {
			id = state.Conversations[current.Conversation].Owner
		}
		if id == 0 || seen[id] {
			return false
		}
		seen[id] = true
		parent := state.Tasks[id]
		if !terminalStatus(parent.Status) && (taskAborted(parent) || taskFailedOutcome(parent)) {
			return true
		}
		if taskBackground(parent) {
			return false
		}
		current = parent
		_ = owners
	}
}

// Direct selection remains distinct from inheritance: a selected member may
// itself be a background task, but ordinary descendants must stop at an
// unselected background or terminal owner. No provisional marks/signals occur.
func taskSelectedFailFastCancellation(state Snapshot, id ID) bool {
	candidate, exists := state.Tasks[id]
	return exists && taskInheritedSelectedFailFastCancellation(state, candidate)
}

func taskInheritedSelectedFailFastCancellation(state Snapshot, candidate Task) bool {
	if terminalStatus(candidate.Status) {
		return false
	}
	if candidate.ID != 0 && taskDirectSelectedFailFastCancellation(state, candidate.ID) {
		return true
	}
	if taskBackground(candidate) {
		return false
	}
	current := candidate
	seen := map[ID]bool{candidate.ID: true}
	for {
		owner := current.Owner
		if owner == 0 {
			owner = state.Conversations[current.Conversation].Owner
		}
		if owner == 0 || seen[owner] {
			return false
		}
		seen[owner] = true
		parent, exists := state.Tasks[owner]
		if !exists || terminalStatus(parent.Status) {
			return false
		}
		if taskDirectSelectedFailFastCancellation(state, owner) {
			return true
		}
		if taskBackground(parent) {
			return false
		}
		current = parent
	}
}

func taskDirectSelectedFailFastCancellation(state Snapshot, id ID) bool {
	candidate, exists := state.Tasks[id]
	if !exists || terminalStatus(candidate.Status) {
		return false
	}
	for _, parent := range state.Tasks {
		if parent.Execution == nil || parent.Execution.Native == nil || parent.Execution.Native.State.Status != "waiting" || parent.Execution.Native.State.Policy != "failFast" {
			continue
		}
		on := parent.Execution.Native.State.On
		selected, failed := false, false
		for _, member := range on {
			if member == id {
				selected = true
			}
			t := state.Tasks[member]
			if taskFailedOutcome(t) || terminalStatus(t.Status) && t.Status != "done" {
				failed = true
			}
		}
		if selected && failed && !taskFailedOutcome(state.Tasks[id]) {
			return true
		}
	}
	return false
}
func taskFailedOutcome(task Task) bool {
	if !taskHasDecidedOutcome(task) {
		return false
	}
	if task.Execution.Native != nil {
		return task.Execution.Native.State.Outcome.Status != "completed"
	}
	return task.Execution.Builtin.Hold.Outcome.Status != "completed"
}
func (s *taskScheduler) prepareTaskWrites(tx *Tx) error {
	if len(tx.writes) == 0 {
		return nil
	}
	// Only terminal execution writes need the final owned-work check. Ordinary
	// progress/document admissions do not need another full candidate replay.
	terminal := false
	for _, write := range tx.writes {
		if write.Task != nil && write.Task.Execution != nil && terminalStatus(write.Task.Status) {
			terminal = true
			break
		}
	}
	if !terminal {
		return nil
	}
	// Hold selection occurs in the deciding adapter BEFORE its task is staged.
	// Never append terminal->completing over a terminal provisional candidate.
	candidate, err := tx.current()
	if err != nil {
		return err
	}
	for _, write := range tx.writes {
		if write.Task != nil && write.Task.Execution != nil && terminalStatus(write.Task.Status) && len(s.ownedLive(candidate, write.Task.ID)) > 0 {
			return reject("terminal task still owns live work")
		}
	}
	return nil
}
func (s *taskScheduler) ownedLive(state Snapshot, owner ID) []ID {
	result := taskOwnedLive(state, owner)
	seen := map[ID]bool{}
	for _, id := range result {
		seen[id] = true
	}
	for id := range s.invocations {
		if id == owner || seen[id] {
			continue
		}
		task, ok := state.Tasks[id]
		if !ok || taskBackground(task) {
			continue
		}
		probe := candidateTables(state)
		task.Status = "running"
		probe.Tasks[id] = task
		for _, member := range taskOwnedLive(probe, owner) {
			if member == id {
				result = append(result, id)
				seen[id] = true
			}
		}
	}
	return result
}

type taskWait struct {
	signal    chan struct{}
	failure   error
	target    ID
	targets   []ID // all dependency edges in one bounded idle registration
	binding   *TaskRuntime
	claims    map[ID]bool
	idleScope *ID
}

func (s *taskScheduler) signalWait(wait *taskWait, err error) {
	if err != nil {
		if wait.failure == nil {
			wait.failure = err
		}
		// Reject the WHOLE registration while still on its original line.
		// Other healthy callers retain shared tickets; adopted tasks are untouched.
		s.releaseWaitClaims(wait)
		delete(s.waiters, wait)
	}
	select {
	case wait.signal <- struct{}{}:
	default:
	}
}
func (s *taskScheduler) notifyWaiters(err error) {
	for wait := range s.waiters {
		s.signalWait(wait, err)
	}
}
func (s *taskScheduler) failTicket(target ID, err error) {
	claims := s.tickets[target]
	delete(s.tickets, target)
	for wait := range claims {
		s.signalWait(wait, err)
	}
}

type taskStop struct {
	runtime *TaskRuntime
	watches []*DocumentWatch
}

func (s *taskScheduler) endOnLine(r *TaskRuntime) {
	if r.ended.Swap(true) {
		return
	}
	stop := taskStop{runtime: r, watches: r.watches}
	r.watches = nil
	for wait := range s.waiters {
		if wait.binding == r {
			s.signalWait(wait, ErrSealed)
			s.releaseWaitClaims(wait)
		}
	}
	s.stopMu.Lock()
	s.stops = append(s.stops, stop)
	s.stopMu.Unlock()
}
func (s *taskScheduler) dispatchStops() {
	s.stopMu.Lock()
	stops := s.stops
	s.stops = nil
	s.stopMu.Unlock()
	for _, stop := range stops {
		for _, watch := range stop.watches {
			watch.Stop()
		}
		stop.runtime.cancel()
	}
}
func (s *taskScheduler) adopt(_ uint64, tx *Tx) {
	for _, stop := range tx.taskStops {
		s.endOnLine(stop.runtime)
	}
	// End capabilities against the ADOPTED lifetime before refreshing dependency
	// frontiers. A newly waiting/held invocation still owns its host permit, but
	// is no longer independent running work that can satisfy a bound wait.
	// Mark cancellation is dispatched off-line, never inline.
	for _, write := range tx.writes {
		if write.Task != nil {
			if taskAborted(*write.Task) && !taskAborted(tx.state.Tasks[write.Task.ID]) {
				s.epoch.Add(1)
				delete(s.retryAfter, write.Task.ID)
			}
			if terminalStatus(write.Task.Status) {
				delete(s.failed, write.Task.ID)
				delete(s.failures, write.Task.ID)
				delete(s.retryAfter, write.Task.ID)
			}
			if r := s.invocations[write.Task.ID]; r != nil {
				if taskAborted(*write.Task) && !r.abortMode {
					r.marked.Store(true)
					if r.definition == nil {
						s.endOnLine(r)
					} else {
						// A native mark rejects writes immediately, but reads remain
						// available until the actual host returns. Keep its permit;
						// cancellation never invents an invocation end.
						s.stopMu.Lock()
						s.stops = append(s.stops, taskStop{runtime: r})
						s.stopMu.Unlock()
					}
				}
				if terminalStatus(write.Task.Status) {
					delete(s.failed, write.Task.ID)
					delete(s.failures, write.Task.ID)
				}
				if terminalStatus(write.Task.Status) || taskHasDecidedOutcome(*write.Task) || write.Task.Status == "waiting" {
					s.endOnLine(r)
				}
			}
		}
	}
	if len(tx.writes) > 0 {
		if tx.externalTaskCommit {
			s.epoch.Add(1)
		}
		if tx.taskAdoptState != nil {
			s.refreshBoundWaits(*tx.taskAdoptState)
		}
		s.notifyWaiters(nil)
		s.kick()
	}
}

var errTaskPassAwaitingWake = errors.New("durable: internal scheduling pass awaits genuine wake")

func (s *taskScheduler) reconcile() error { return s.reconcilePass(false) }
func (s *taskScheduler) reconcilePass(requireEnabled bool) error {
	cancels := []context.CancelFunc{}
	var attemptEpoch uint64
	var retryAtAdmission *uint64
	_, err := s.h.session.taskCommit(context.Background(), func(tx *Tx) error {
		retryAtAdmission = s.reconcileRetry
		attemptEpoch = s.epoch.Load()
		tx.taskRollback = func(failure error) {
			if !errors.Is(failure, errTaskPassAwaitingWake) {
				epoch := attemptEpoch
				s.reconcileRetry = &epoch
			}
		}
		if s.reconcileRetry != nil && attemptEpoch <= *s.reconcileRetry {
			return errTaskPassAwaitingWake
		}
		if s.sealed.Load() || requireEnabled && !s.enabled.Load() {
			return nil
		}
		state := tx.state
		// Withdraw one indivisible queued-input unit before cascading marks:
		// generation+submission+inbox must never be partially admitted. A unit
		// larger than configured budgets rejects explicitly, not split apart.
		if err := s.h.withdrawScopedInputBatch(tx, 0, false, map[ID]bool{}, true); err != nil {
			return err
		}
		if len(tx.writes) > 0 {
			return nil
		}
		for _, id := range ids(state.Tasks) {
			task := state.Tasks[id]
			if terminalStatus(task.Status) {
				continue
			}
			if taskBelowCancelled(state, task) && !taskAborted(task) {
				task = markTask(task)
				if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
					return err
				}
				return nil // next adoption continues the bounded cascade
			}
			if taskAborted(task) {
				if r := s.invocations[id]; r != nil && !r.abortMode {
					cancels = append(cancels, r.cancel)
				}
			}
			if task.Execution != nil && task.Execution.Native != nil && task.Execution.Native.State.Status == "waiting" && task.Execution.Native.State.Policy == "failFast" {
				members := task.Execution.Native.State.On
				failed := false
				for _, member := range members {
					if taskFailedOutcome(state.Tasks[member]) || terminalStatus(state.Tasks[member].Status) && state.Tasks[member].Status != "done" {
						failed = true
					}
				}
				if failed {
					for _, member := range members {
						child := state.Tasks[member]
						if !terminalStatus(child.Status) && !taskFailedOutcome(child) && !taskAborted(child) {
							child = markTask(child)
							if err := tx.stage(Write{Op: "put-task", Task: &child}); err != nil {
								return err
							}
							return nil // selected surviving siblings marked on next pass
						}
					}
				}
			}
		}
		// Mark/withdraw prefixes have already returned for adoption. No later
		// final cleanup may reject earlier independent cascade work.
		// One bottom-up terminal transition per pass, including owned conversations.
		{
			candidate, err := tx.current()
			if err != nil && len(tx.writes) > 0 {
				return err
			}
			if len(tx.writes) == 0 {
				candidate = state
			}
			for _, id := range ids(candidate.Tasks) {
				task, err := copyTask(candidate.Tasks[id], tx.limits)
				if err != nil {
					return err
				}
				if task.Status != "completing" || !taskHasDecidedOutcome(task) || len(s.ownedLive(candidate, id)) > 0 || s.invocations[id] != nil {
					continue
				}
				if task.Execution.Native != nil {
					task.Status = outcomeRawStatus(task.Execution.Native.State.Outcome)
					task.Execution.Native.State.Status = "terminal"
					if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
						return err
					}
				} else {
					if err := s.h.finalizeBuiltinHold(tx, task); err != nil {
						return err
					}
				}
				// A terminal+all-task-document retirement is one indivisible
				// transition. Adopt it alone; later passes continue bottom-up.
				return nil
			}
		}
		return nil
	})
	s.h.session.taskBookkeeping(func() {
		if err == nil && s.reconcileRetry == retryAtAdmission {
			// Do not clear a newer failed pass that adopted its barrier after
			// this successful pass released the Session line.
			s.reconcileRetry = nil
		} // Failure barrier was installed before the original Session.leave.
	})
	if err == nil {
		for _, cancel := range cancels {
			cancel()
		}
	}
	return err
}
func markTask(task Task) Task {
	if task.Execution != nil && task.Execution.Native != nil {
		native := *task.Execution.Native
		native.AbortRequested = true
		execution := *task.Execution
		execution.Native = &native
		task.Execution = &execution
	} else {
		checkpoint := make(JSON, len(task.Checkpoint)+1)
		for k, v := range task.Checkpoint {
			checkpoint[k] = v
		}
		checkpoint["abort"] = true
		task.Checkpoint = checkpoint
		if task.Execution != nil {
			builtin := *task.Execution.Builtin
			builtin.AbortRequested = true
			execution := *task.Execution
			execution.Builtin = &builtin
			task.Execution = &execution
		}
	}
	return task
}

// A bound wait claims every unreserved eligible frontier permit atomically.
// This is re-evaluated at each adopted state change, including a formerly
// running target becoming waiting/completing. Partial claims never survive.
func (s *taskScheduler) admitBoundWait(state Snapshot, wait *taskWait, id ID) error {
	return s.admitBoundTargets(state, wait, []ID{id})
}

func (s *taskScheduler) admitBoundTargets(state Snapshot, wait *taskWait, targets []ID) error {
	// Remove only this registration's old claims before planning the WHOLE new
	// frontier. No partial claim is ever published, even on failed refresh.
	s.releaseWaitClaims(wait)
	caller := wait.binding
	id := wait.target
	if id == caller.taskID {
		return &InvocationWaitRequiresYield{Task: id}
	}
	owners, err := taskOwnerChain(state, state.Tasks[caller.taskID])
	if err != nil {
		return err
	}
	if owners[id] {
		return &InvocationWaitRequiresYield{Task: id}
	}
	if !s.waiters[wait] && len(s.waiters) >= s.h.session.limits.MaxPage {
		return reject("task waiter capacity")
	}
	for _, target := range targets {
		if s.boundWaitCycle(state, caller.taskID, target, map[ID]bool{}) {
			return &InvocationWaitRequiresYield{Task: target}
		}
	}
	snapshot, err := s.h.options.Registry.taskSnapshot(s.h.session.limits)
	if err != nil {
		return err
	}
	needed := map[ID]bool{}
	claimsNeeded := map[ID]bool{}
	visiting := map[ID]bool{}
	var visit func(ID) error
	visit = func(targetID ID) error {
		if targetID == caller.taskID || owners[targetID] || visiting[targetID] {
			return &InvocationWaitRequiresYield{Task: id}
		}
		task, ok := state.Tasks[targetID]
		if !ok {
			return reject("unknown task")
		}
		if terminalStatus(task.Status) {
			return nil
		}
		visiting[targetID] = true
		defer delete(visiting, targetID)
		if runtime := s.invocations[targetID]; runtime != nil && !runtime.ended.Load() && task.Status == "running" && !taskHasDecidedOutcome(task) && !taskAborted(task) {
			if s.boundWaitCycle(state, caller.taskID, targetID, map[ID]bool{}) {
				return &InvocationWaitRequiresYield{Task: id}
			}
			return nil
		}
		if taskHasDecidedOutcome(task) {
			for _, child := range s.ownedLive(state, targetID) {
				if err := visit(child); err != nil {
					return err
				}
			}
			return nil
		}
		if task.Kind == "pi.generation" {
			var cp generationCheckpoint
			if err := fromObject(task.Checkpoint, &cp, s.h.session.limits); err != nil {
				return err
			}
			if cp.Phase == "tools" {
				children, err := s.toolRoundChildren(state, task, cp)
				if err != nil {
					return err
				}
				for _, child := range children {
					if !terminalStatus(child.Status) || s.invocations[child.ID] != nil {
						return visit(child.ID)
					}
				}
				if owned := s.ownedLive(state, targetID); len(owned) > 0 {
					for _, child := range owned {
						if err := visit(child); err != nil {
							return err
						}
					}
					return nil
				}
			}
		}
		if task.Execution != nil && task.Execution.Native != nil && task.Execution.Native.State.Status == "waiting" && !taskAborted(task) {
			live := false
			for _, child := range task.Execution.Native.State.On {
				if !terminalStatus(state.Tasks[child].Status) {
					live = true
					if err := visit(child); err != nil {
						return err
					}
				}
			}
			if live {
				return nil
			}
		}
		if taskAborted(task) && len(s.ownedLive(state, targetID)) > 0 {
			for _, child := range s.ownedLive(state, targetID) {
				if err := visit(child); err != nil {
					return err
				}
			}
			return nil
		}
		// A returned handler still holding its real permit is join work, not a new
		// executor. Wait for its observed return before another reservation.
		if runtime := s.invocations[targetID]; runtime != nil {
			return nil
		}
		if !s.runnable(state, task) {
			return &InvocationWaitRequiresYield{Task: id}
		}
		if task.Execution != nil && task.Execution.Native != nil {
			def := snapshot.Task(task.Kind)
			if def == nil || def.Version() < task.Execution.Native.Version || def.Version() > task.Execution.Native.Version && def.options.Migrate == nil || s.failed[targetID] == def {
				return &InvocationWaitRequiresYield{Task: id}
			}
		}
		claimsNeeded[targetID] = true
		if s.tickets[targetID] == nil {
			needed[targetID] = true
		}
		return nil
	}
	for _, target := range targets {
		if err := visit(target); err != nil {
			return err
		}
	}
	if len(s.invocations)+len(s.tickets)+len(needed) > s.capacity() {
		return &InvocationWaitRequiresYield{Task: id}
	}
	s.releaseWaitClaims(wait)
	wait.claims = map[ID]bool{}
	for target := range claimsNeeded {
		if s.tickets[target] == nil {
			s.tickets[target] = map[*taskWait]bool{}
		}
		s.tickets[target][wait] = true
		wait.claims[target] = true
	}
	// claimsNeeded includes existing tickets as well as new ones: each bound
	// registration owns its claim, independently of other callers' cancellation.
	wait.targets = append([]ID(nil), targets...)
	if len(needed) > 0 {
		s.kick()
	}
	return nil
}

func (s *taskScheduler) releaseWaitClaims(wait *taskWait) {
	for target := range wait.claims {
		claims := s.tickets[target]
		delete(claims, wait)
		if len(claims) == 0 {
			delete(s.tickets, target)
		}
	}
	wait.claims = nil
}
func (s *taskScheduler) boundWaitCycle(state Snapshot, caller, target ID, seen map[ID]bool) bool {
	if target == caller {
		return true
	}
	if seen[target] {
		return false
	}
	seen[target] = true
	// Volatile roots can lead THROUGH persisted joins before reaching another
	// active invocation. Include those durable edges in the same cycle graph.
	task, exists := state.Tasks[target]
	if exists && !terminalStatus(task.Status) {
		var dependencies []ID
		if taskHasDecidedOutcome(task) || taskAborted(task) {
			dependencies = s.ownedLive(state, target)
		}
		if !taskAborted(task) && !taskHasDecidedOutcome(task) && task.Execution != nil && task.Execution.Native != nil && task.Execution.Native.State.Status == "waiting" {
			dependencies = append(dependencies, task.Execution.Native.State.On...)
		}
		if task.Kind == "pi.generation" && !taskHasDecidedOutcome(task) {
			var cp generationCheckpoint
			if fromObject(task.Checkpoint, &cp, s.h.session.limits) == nil && cp.Phase == "tools" {
				dependencies = append(dependencies, cp.Children...)
			}
		}
		for _, dependency := range dependencies {
			if child, ok := state.Tasks[dependency]; ok && (!terminalStatus(child.Status) || s.invocations[dependency] != nil) && s.boundWaitCycle(state, caller, dependency, seen) {
				return true
			}
		}
	}
	for wait := range s.waiters {
		if wait.binding == nil || wait.binding.taskID != target || wait.failure != nil {
			continue
		}
		for _, dependency := range wait.targets {
			if s.boundWaitCycle(state, caller, dependency, seen) {
				return true
			}
		}
	}
	return false
}

// refreshBoundWaits runs on adoption and actual permit release BEFORE ordinary
// selection. One idle registration plans all current scope edges atomically.
func (s *taskScheduler) refreshBoundWaits(state Snapshot) {
	for wait := range s.waiters {
		if wait.binding == nil || wait.failure != nil {
			continue
		}
		if err := wait.binding.check(); err != nil {
			s.releaseWaitClaims(wait)
			s.signalWait(wait, err)
			continue
		}
		targets := []ID{wait.target}
		if wait.idleScope != nil {
			targets = s.idleTargets(state, *wait.idleScope)
		}
		if err := s.admitBoundTargets(state, wait, targets); err != nil {
			s.signalWait(wait, err)
		}
	}
}
func (s *taskScheduler) idleTargets(state Snapshot, conversation ID) []ID {
	targets := []ID{}
	for _, id := range ids(state.Tasks) {
		task := state.Tasks[id]
		if (!terminalStatus(task.Status) || s.invocations[id] != nil) && taskInScope(state, task, conversation, false) {
			targets = append(targets, id)
		}
	}
	return targets
}

// Validate the ENTIRE persisted round before selecting its first live member.
// A corrupt suffix cannot admit a valid-looking prefix effect.
func (s *taskScheduler) toolRoundChildren(state Snapshot, parent Task, cp generationCheckpoint) ([]Task, error) {
	children := make([]Task, 0, len(cp.Children))
	seen := map[ID]bool{}
	for _, id := range cp.Children {
		child, ok := state.Tasks[id]
		if seen[id] || !ok || child.Kind != "pi.tool" || child.Owner != parent.ID || child.Conversation != parent.Conversation {
			return nil, reject("invalid tool round child")
		}
		seen[id] = true
		var toolCP toolCheckpoint
		if err := fromObject(child.Checkpoint, &toolCP, s.h.session.limits); err != nil {
			return nil, err
		}
		if toolCP.CallID == "" {
			return nil, reject("tool round call identity")
		}
		if terminalStatus(child.Status) || toolCP.Result != nil {
			if toolCP.Result == nil || toolCP.Result.Role != goai.RoleToolResult || toolCP.Result.ToolCallID != toolCP.CallID || toolCP.Result.ToolName != toolCP.Offer.Name {
				return nil, reject("tool round result identity")
			}
			value, err := dtoObject(*toolCP.Result, s.h.session.limits)
			if err != nil {
				return nil, err
			}
			found := false
			for _, entry := range state.Entries {
				// The actual pre-S2e producer omitted ByTask. Only an absent
				// execution envelope may use that historical placement; native
				// Holds retain their strict producing-task reference validation.
				attributed := entry.ByTask == child.ID || child.Execution == nil && entry.ByTask == 0
				if entry.Conversation == child.Conversation && attributed && entry.Kind == "message" && equalJSONValue(entry.Value, value) {
					found = true
					break
				}
			}
			if !found {
				return nil, reject("tool round receipt missing")
			}
		}
		children = append(children, child)
	}
	return children, nil
}

// builtinDecision is only for scheduler-written corruption/fault or an absent
// built-in phase adapter. Ordinary tool errors keep their accepted receipt path.
func (s *taskScheduler) builtinDecision(tx *Tx, task Task, outcome TaskOutcome) error {
	owned, err := copyTask(task, tx.limits)
	if err != nil {
		return err
	}
	task = owned
	if taskHasDecidedOutcome(task) || terminalStatus(task.Status) {
		return nil
	}
	hold := &BuiltinTaskHold{Stage: "held", Outcome: outcome, FinalStatus: "failed", Conversation: task.Conversation, Owner: task.Owner}
	switch task.Kind {
	case "pi.generation":
		var cp generationCheckpoint
		if err := fromObject(task.Checkpoint, &cp, tx.limits); err != nil {
			return err
		}
		hold.Action = "scheduler-generation"
		hold.Submission = cp.Submission
	case "pi.tool":
		var cp toolCheckpoint
		if err := fromObject(task.Checkpoint, &cp, tx.limits); err != nil {
			return err
		}
		hold.Action = "scheduler-tool"
		hold.CallID = cp.CallID
	default:
		return reject("unknown builtin adapter")
	}
	metadata := &BuiltinTaskExecution{}
	if task.Execution != nil {
		*metadata = *task.Execution.Builtin
	}
	metadata.Memos = nil
	metadata.Hold = hold
	task.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: metadata}
	task.Status = "completing"
	candidate := tx.state
	if len(tx.writes) > 0 {
		candidate, err = tx.current()
		if err != nil {
			return err
		}
	}
	if len(s.ownedLive(candidate, task.ID)) == 0 {
		if err := s.h.finalizeBuiltinHold(tx, task); err != nil {
			return err
		}
	} else if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
		return err
	}
	if r := s.invocations[task.ID]; r != nil {
		tx.taskStops = append(tx.taskStops, taskStop{runtime: r})
	}
	return nil
}
