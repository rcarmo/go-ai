package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// InvocationWaitRequiresYield is a bounded-capacity rejection, never a durable
// cancellation. Commit waiting and return, or return a tool result into Hold.
type InvocationWaitRequiresYield struct{ Task ID }

func (*InvocationWaitRequiresYield) Error() string { return "durable: invocation_wait_requires_yield" }

type TaskRuntime struct {
	harness              *Harness
	taskID, conversation ID
	context              context.Context
	cancel               context.CancelFunc
	ended                atomic.Bool
	marked               atomic.Bool
	registry             TaskRegistrySnapshot
	phaseMu              sync.RWMutex
	definition           *TaskDefinition
	abortMode            bool
	admissionFailed      bool // host-owned until return; published on the line
	admissionEpoch       uint64
	fallbackEpoch        *uint64 // line-owned one deciding/fallback attempt boundary
	handoverReported     bool
	handoverToken        *TaskDefinition // last resolved token only, bounded by invocation
	done                 chan struct{}
	watches              []*DocumentWatch // Session-line owned; drained after host return
}

func (r *TaskRuntime) check() error {
	if r == nil || r.ended.Load() {
		return ErrSealed
	}
	return nil
}
func (r *TaskRuntime) TaskID() ID               { return r.taskID }
func (r *TaskRuntime) ConversationID() ID       { return r.conversation }
func (r *TaskRuntime) Context() context.Context { return r.context }
func (r *TaskRuntime) Registry() (TaskRegistrySnapshot, error) {
	if err := r.check(); err != nil {
		return TaskRegistrySnapshot{}, err
	}
	r.phaseMu.RLock()
	defer r.phaseMu.RUnlock()
	return r.registry, nil
}
func (r *TaskRuntime) Commit(ctx context.Context, change func(*Tx, TaskRecord) (*TaskState, error)) error {
	if err := r.check(); err != nil {
		return err
	}
	if change == nil {
		return reject("nil runtime commit")
	}
	_, err := r.harness.session.invocationCommit(ctx, r.taskID, func(tx *Tx) error {
		if err := r.check(); err != nil {
			return err
		}
		if r.harness.closing.Load() {
			return ErrClosed
		}
		task, ok := tx.state.Tasks[r.taskID]
		if !ok || terminalStatus(task.Status) || taskHasDecidedOutcome(task) || task.Status != "running" {
			return ErrSealed
		}
		if !r.abortMode && taskAborted(task) {
			return reject("task has durable abort mark")
		}
		view, err := CanonicalTask(task, tx.limits)
		if err != nil {
			return err
		}
		tx.taskConversation = r.conversation
		tx.byTask = r.taskID
		next, err := change(tx, view)
		if err != nil {
			return err
		}
		if next == nil {
			return nil
		}
		if task.Execution == nil || task.Execution.Native == nil {
			return reject("builtin runtime cannot replace its execution state")
		}
		state, err := copyTaskState(*next, tx.limits)
		if err != nil {
			return err
		}
		if state.Status != "running" && state.Status != "waiting" && state.Status != "terminal" {
			return reject("runtime next state union")
		}
		if state.Status == "waiting" && r.abortMode {
			return reject("abort handler cannot wait")
		}
		if state.Outcome != nil && (state.Outcome.Status == "faulted" || state.Outcome.Status == "orphaned") {
			return reject("scheduler-only outcome")
		}
		native := *task.Execution.Native
		native.State = state
		if state.Status == "terminal" {
			native.Memos = nil
			task.Status = outcomeRawStatus(state.Outcome)
			candidate := tx.state
			if len(tx.writes) > 0 {
				candidate, err = tx.current()
				if err != nil {
					return err
				}
			}
			if len(r.harness.scheduler.ownedLive(candidate, task.ID)) > 0 {
				native.State.Status = "completing"
				task.Status = "completing"
			}
		} else {
			task.Status = state.Status
		}
		task.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &native}
		return tx.stage(Write{Op: "put-task", Task: &task})
	})
	return err
}
func (r *TaskRuntime) Memo(ctx context.Context, name string) (any, bool, error) {
	if err := r.check(); err != nil {
		return nil, false, err
	}
	var value any
	var found bool
	err := r.harness.session.readTasks(ctx, func(state Snapshot) error {
		if err := r.check(); err != nil {
			return err
		}
		task := state.Tasks[r.taskID]
		var memos map[string]*TaskValue
		if task.Execution != nil {
			if task.Execution.Native != nil {
				memos = task.Execution.Native.Memos
			} else {
				memos = task.Execution.Builtin.Memos
			}
		}
		if v, ok := memos[name]; ok {
			copy, err := copyTaskValue(v, r.harness.session.limits)
			if err != nil {
				return err
			}
			value = copy.Value
			found = true
		}
		return nil
	})
	return value, found, err
}
func (r *TaskRuntime) MemoCandidate(ctx context.Context, name string, candidate any) (any, error) {
	var winner any
	if err := r.check(); err != nil {
		return nil, err
	}
	_, err := r.harness.session.invocationCommit(ctx, r.taskID, func(tx *Tx) error {
		if err := r.check(); err != nil {
			return err
		}
		if r.harness.closing.Load() {
			return ErrClosed
		}
		task, ok := tx.state.Tasks[r.taskID]
		if !ok || task.Status != "running" || taskHasDecidedOutcome(task) {
			return ErrSealed
		}
		if !r.abortMode && taskAborted(task) {
			return reject("task aborted")
		}
		if err := (&jsonBudget{l: tx.limits}).str(name); err != nil {
			return err
		}
		memos := map[string]*TaskValue{}
		if task.Execution != nil && task.Execution.Native != nil {
			for k, v := range task.Execution.Native.Memos {
				memos[k] = v
			}
			if v, ok := memos[name]; ok {
				copy, err := copyTaskValue(v, tx.limits)
				if err != nil {
					return err
				}
				winner = copy.Value
				return nil
			}
			value, err := copyTaskValue(&TaskValue{Present: true, Value: candidate}, tx.limits)
			if err != nil {
				return err
			}
			memos[name] = value
			native := *task.Execution.Native
			native.Memos = memos
			task.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &native}
		} else {
			builtin := &BuiltinTaskExecution{}
			if task.Execution != nil {
				*builtin = *task.Execution.Builtin
			}
			for k, v := range builtin.Memos {
				memos[k] = v
			}
			if v, ok := memos[name]; ok {
				copy, err := copyTaskValue(v, tx.limits)
				if err != nil {
					return err
				}
				winner = copy.Value
				return nil
			}
			value, err := copyTaskValue(&TaskValue{Present: true, Value: candidate}, tx.limits)
			if err != nil {
				return err
			}
			memos[name] = value
			builtin.Memos = memos
			task.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: builtin}
		}
		if err := tx.stage(Write{Op: "put-task", Task: &task}); err != nil {
			return err
		}
		copy, err := copyTaskValue(memos[name], tx.limits)
		if err != nil {
			return err
		}
		winner = copy.Value
		return nil
	})
	return winner, err
}
func (r *TaskRuntime) Task(ctx context.Context, id ID) (TaskRecord, bool, error) {
	if err := r.check(); err != nil {
		return TaskRecord{}, false, err
	}
	var view TaskRecord
	var found bool
	err := r.harness.session.readTasks(ctx, func(state Snapshot) error {
		if err := r.check(); err != nil {
			return err
		}
		task, ok := state.Tasks[id]
		found = ok
		if !ok {
			return nil
		}
		var err error
		view, err = CanonicalTask(task, r.harness.session.limits)
		return err
	})
	return view, found, err
}
func (r *TaskRuntime) WaitForTask(ctx context.Context, id ID) (TaskRecord, error) {
	if err := r.check(); err != nil {
		return TaskRecord{}, err
	}
	if ctx == nil {
		return TaskRecord{}, reject("nil task wait context")
	}
	combined, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(r.context, cancel)
	defer func() {
		stop()
		cancel()
		r.harness.scheduler.kick()
	}()
	return r.harness.waitTask(combined, id, r)
}
func (r *TaskRuntime) Outcomes(ctx context.Context, ids []ID) ([]TaskOutcome, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	var out []TaskOutcome
	err := r.harness.session.readTasks(ctx, func(state Snapshot) error {
		if err := r.check(); err != nil {
			return err
		}
		l := r.harness.session.limits
		if len(ids) > l.MaxPage {
			return reject("task outcome collection limit")
		}
		candidate := make([]TaskOutcome, 0, len(ids))
		for _, id := range ids {
			raw, ok := state.Tasks[id]
			if !ok {
				return reject("task outcome not terminal")
			}
			task, err := CanonicalTask(raw, l)
			if err != nil {
				return err
			}
			if task.State.Status != "terminal" || task.State.Outcome == nil {
				return reject("task outcome not terminal")
			}
			owned, err := copyTaskOutcome(task.State.Outcome, l)
			if err != nil {
				return err
			}
			candidate = append(candidate, *owned)
		}
		// Duplicates retain caller order but own independent placements. Bound
		// the complete response, not just individual stored task outcomes.
		if _, err := encodeBounded(JSON{"outcomes": candidate}, l, l.MaxFramePayloadBytes); err != nil {
			return err
		}
		out = candidate
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (r *TaskRuntime) Now() (int64, error) {
	if err := r.check(); err != nil {
		return 0, err
	}
	if r.harness.options.Now != nil {
		return r.harness.options.Now(), nil
	}
	return time.Now().UnixMilli(), nil
}
func (r *TaskRuntime) Report(err error) error {
	if e := r.check(); e != nil {
		return e
	}
	r.harness.scheduler.report(err)
	return nil
}
func (r *TaskRuntime) SleepUntil(ctx context.Context, until int64) error {
	if err := r.check(); err != nil {
		return err
	}
	if ctx == nil {
		return reject("nil sleep context")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.context.Err(); err != nil {
			return err
		}
		now, err := r.Now()
		if err != nil {
			return err
		}
		if now >= until {
			return nil
		}
		timer := time.NewTimer(taskSleepDelay(now, until))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-r.context.Done():
			timer.Stop()
			return r.context.Err()
		}
	}
}

// Called only for until>now. Modular unsigned subtraction is the exact positive
// mathematical distance across int64 clock signs; clamp BEFORE conversion.
func taskSleepDelay(now, until int64) time.Duration {
	distance := uint64(until) - uint64(now)
	if distance >= 1000 {
		return time.Second
	}
	return time.Duration(distance) * time.Millisecond
}
func (r *TaskRuntime) ContextView(ctx context.Context, conversation, at ID) (ContextView, error) {
	if err := r.check(); err != nil {
		return ContextView{}, err
	}
	view, err := r.harness.session.ContextView(ctx, conversation, at)
	if err == nil {
		err = r.check()
	}
	return view, err
}
func (r *TaskRuntime) SnapshotDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (JSON, bool, error) {
	if err := r.check(); err != nil {
		return nil, false, err
	}
	v, ok, err := r.harness.session.SnapshotDefinition(ctx, def, owner, key)
	if err == nil {
		err = r.check()
	}
	return v, ok, err
}
func (r *TaskRuntime) SnapshotDefinitionAsOf(ctx context.Context, def *DocumentDefinition, owner ID, key *string, at ID) (JSON, bool, error) {
	if err := r.check(); err != nil {
		return nil, false, err
	}
	v, ok, err := r.harness.session.SnapshotDefinitionAsOf(ctx, def, owner, key, at)
	if err == nil {
		err = r.check()
	}
	return v, ok, err
}
func (r *TaskRuntime) WatchDefinition(ctx context.Context, def *DocumentDefinition, owner ID, key *string) (*DocumentWatch, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	if r.harness.closing.Load() {
		return nil, ErrClosed
	}
	watch, _, err := r.harness.session.WatchDefinition(ctx, def, owner, key)
	if err != nil || watch == nil {
		return watch, err
	}
	return r.attachWatch(ctx, watch)
}

// attachWatch rechecks ownership when an earlier Session acquisition completes.
// Rejected attachment always retires the acquired process watch off-line.
func (r *TaskRuntime) attachWatch(ctx context.Context, watch *DocumentWatch) (*DocumentWatch, error) {
	err := r.harness.session.readTasks(ctx, func(Snapshot) error {
		if err := r.check(); err != nil {
			return err
		}
		if r.harness.closing.Load() {
			return ErrClosed
		}
		live := r.watches[:0]
		for _, prior := range r.watches {
			select {
			case <-prior.Closed():
			default:
				live = append(live, prior)
			}
		}
		r.watches = live
		if len(r.watches) >= r.harness.session.limits.MaxPage {
			return reject("invocation watch capacity")
		}
		r.watches = append(r.watches, watch)
		return nil
	})
	if err != nil {
		watch.Stop()
		return nil, err
	}
	return watch, nil
}
func (r *TaskRuntime) Entry(ctx context.Context, id ID) (Entry, bool, error) {
	if err := r.check(); err != nil {
		return Entry{}, false, err
	}
	var result Entry
	var found bool
	err := r.harness.session.readTasks(ctx, func(state Snapshot) error {
		if err := r.check(); err != nil {
			return err
		}
		if !entryVisible(state, r.conversation, id) {
			return nil
		}
		var err error
		result, err = copyEntry(state.Entries[id], r.harness.session.limits)
		found = err == nil
		return err
	})
	return result, found, err
}
func (r *TaskRuntime) TypedEntry(ctx context.Context, def *EntryDefinition, id ID) (Entry, bool, error) {
	if err := r.check(); err != nil {
		return Entry{}, false, err
	}
	if def == nil {
		return Entry{}, false, reject("nil entry definition")
	}
	entry, ok, err := r.Entry(ctx, id)
	return entry, ok && def.Is(entry), err
}

// InvocationConversation is a distinct bound capability; ordinary stateless
// ConversationHandle retains its legacy struct shape and host lifetime.
type InvocationConversation struct {
	runtime *TaskRuntime
	handle  *ConversationHandle
}

func (r *TaskRuntime) Conversation(ctx context.Context, id ID) (*InvocationConversation, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	handle, err := r.harness.Conversation(ctx, id)
	if err != nil {
		return nil, err
	}
	return &InvocationConversation{runtime: r, handle: handle}, r.check()
}
func (c *InvocationConversation) ID() ID { return c.handle.id }

func (c *InvocationConversation) Submit(ctx context.Context, input Input) (*InvocationSubmission, error) {
	if err := c.runtime.check(); err != nil {
		return nil, err
	}
	sub, err := c.handle.submitBound(ctx, input, c.runtime)
	if err != nil {
		return nil, err
	}
	return &InvocationSubmission{runtime: c.runtime, handle: sub}, nil
}
func (c *InvocationConversation) WaitForIdle(ctx context.Context) error {
	return c.handle.h.waitIdleScope(ctx, c.handle.id, c.runtime)
}
func (c *InvocationConversation) Abort(ctx context.Context) error {
	if err := c.runtime.check(); err != nil {
		return err
	}
	return c.handle.abortBound(ctx, ConversationAbortOptions{}, c.runtime)
}

type InvocationSubmission struct {
	runtime *TaskRuntime
	handle  *SubmissionHandle
}

func (s *InvocationSubmission) ID() ID { return s.handle.id }
func (s *InvocationSubmission) Wait(ctx context.Context) (Settlement, error) {
	if err := s.runtime.check(); err != nil {
		return Settlement{}, err
	}
	for {
		var target ID
		settled := false
		err := s.runtime.harness.session.readTasks(ctx, func(state Snapshot) error {
			if err := s.runtime.check(); err != nil {
				return err
			}
			sub, ok := state.Submissions[s.handle.id]
			if !ok {
				return reject("unknown submission")
			}
			if terminalStatus(sub.Status) {
				settled = true
				return nil
			}
			for _, id := range ids(state.Tasks) {
				task := state.Tasks[id]
				if task.Kind != "pi.generation" {
					continue
				}
				var cp generationCheckpoint
				if fromObject(task.Checkpoint, &cp, s.runtime.harness.session.limits) == nil && generationIncludesSubmission(cp, sub.ID) && !terminalStatus(task.Status) {
					target = task.ID
					break
				}
			}
			if target == 0 {
				return reject("submission task unavailable")
			}
			return nil
		})
		if err != nil {
			return Settlement{}, err
		}
		if settled {
			break
		}
		if target != 0 {
			if _, err := s.runtime.WaitForTask(ctx, target); err != nil {
				return Settlement{}, err
			}
		}
		// A queued placeholder may have been joined to another generation. Resolve
		// again until its submission settles; never treat placeholder retirement as
		// the answer boundary.
	}
	var result Settlement
	err := s.runtime.harness.session.readTasks(ctx, func(state Snapshot) error {
		if err := s.runtime.check(); err != nil {
			return err
		}
		sub, ok := state.Submissions[s.handle.id]
		if !ok || !terminalStatus(sub.Status) {
			return reject("submission not settled")
		}
		value, err := copyObject(sub.Value, s.runtime.harness.session.limits)
		if err != nil {
			return err
		}
		sub.Value = value
		result.Submission = sub
		for _, id := range ids(state.Tasks) {
			task := state.Tasks[id]
			if task.Kind != "pi.generation" {
				continue
			}
			var cp generationCheckpoint
			if fromObject(task.Checkpoint, &cp, s.runtime.harness.session.limits) == nil && generationIncludesSubmission(cp, sub.ID) && cp.Phase != "queued" {
				result.Task, err = copyTask(task, s.runtime.harness.session.limits)
				if err != nil {
					return err
				}
				break
			}
		}
		if value, ok := sub.Value["message"].(map[string]any); ok {
			var message MessageReceipt
			if err := fromObject(JSON(value), &message, s.runtime.harness.session.limits); err != nil {
				return err
			}
			result.Message = &message
		}
		return nil
	})
	return result, err
}
