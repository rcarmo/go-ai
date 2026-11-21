package durable

import (
	"context"
	"maps"
	"slices"

	goai "github.com/rcarmo/go-ai"
)

// SignalResult reports what a delivery did, not whether a task consumed it.
type SignalResult string

const (
	SignalDelivered SignalResult = "delivered"
	SignalDuplicate SignalResult = "duplicate"
	SignalInvalid   SignalResult = "invalid"
	SignalClosed    SignalResult = "closed"
	SignalNotFound  SignalResult = "not_found"
)

// Signal resumes scheduling and delivers a JSON value in a Session commit.
func (h *Harness) Signal(ctx context.Context, id ID, token *SignalToken, key string, value any) (SignalResult, error) {
	if err := h.Resume(ctx); err != nil {
		return "", err
	}
	var result SignalResult
	_, err := h.commitTasks(ctx, 0, func(tx *Tx) error {
		var err error
		result, err = tx.Signal(id, token, key, value)
		return err
	})
	if err != nil {
		return "", err
	}
	return result, nil
}

// Signal delivers to the task as this transaction currently sees it. Only a
// successful delivery writes a memo; duplicate deliveries retain the winner.
func (t *Tx) Signal(id ID, token *SignalToken, key string, value any) (SignalResult, error) {
	if err := t.enterTableRead("signal", taskKey(id)); err != nil {
		return "", err
	}
	defer t.leave()
	state := t.state
	if len(t.writes) != 0 || len(t.taskCandidates) != 0 {
		var err error
		state, err = t.current()
		if err != nil {
			return "", err
		}
	}
	task, found := state.Tasks[id]
	if !found {
		return SignalNotFound, nil
	}
	result, replacement, err := t.deliverSignal(task, token, key, value)
	if err != nil || replacement == nil {
		return result, err
	}
	if err := t.stage(Write{Op: "put-task", Task: replacement}); err != nil {
		return "", err
	}
	return result, nil
}

// SignalDelivery describes one delivery in a batch started before table writes.
type SignalDelivery struct {
	TaskID ID
	Token  *SignalToken
	Key    string
	Value  any
}

// Signals admits several deliveries together. It is the synchronous form of
// starting the source transaction's signal reads together: later deliveries
// see candidates staged by earlier ones. Sequential Signal calls still use
// the ordinary read-after-write guard.
func (t *Tx) Signals(deliveries ...SignalDelivery) ([]SignalResult, error) {
	if len(deliveries) > t.limits.MaxMembers {
		return nil, reject("signal delivery count limit")
	}
	keys := make([]string, 0, len(deliveries))
	seen := make(map[ID]bool, len(deliveries))
	for _, request := range deliveries {
		if !seen[request.TaskID] {
			seen[request.TaskID] = true
			keys = append(keys, taskKey(request.TaskID))
		}
	}
	if err := t.enterTableRead("signal", keys...); err != nil {
		return nil, err
	}
	defer t.leave()
	results := make([]SignalResult, 0, len(deliveries))
	for _, request := range deliveries {
		task, found := t.state.Tasks[request.TaskID]
		if !found {
			results = append(results, SignalNotFound)
			continue
		}
		if candidate, exists := t.taskCandidates[request.TaskID]; exists {
			var err error
			task, err = nativeTask(candidate, t.limits)
			if err != nil {
				return nil, err
			}
		}
		result, replacement, err := t.deliverSignal(task, request.Token, request.Key, request.Value)
		if err != nil {
			return nil, err
		}
		if replacement != nil {
			candidate, err := CanonicalTask(*replacement, t.limits)
			if err != nil {
				return nil, err
			}
			if err := t.stageCanonicalTask(candidate); err != nil {
				return nil, err
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func (t *Tx) deliverSignal(task Task, token *SignalToken, key string, value any) (SignalResult, *Task, error) {
	if terminalStatus(task.Status) || taskHasDecidedOutcome(task) || taskAborted(task) {
		return SignalClosed, nil, nil
	}
	if key == "" {
		return SignalInvalid, nil, nil
	}
	owned, err := ownJSONValue(value, t.limits)
	if err != nil {
		return SignalInvalid, nil, nil
	}
	if token == nil || token.definition.Schema == nil {
		return "", nil, reject("invalid signal token")
	}
	if !goai.CheckJSONSchemaValue(map[string]any(token.definition.Schema), owned) {
		return SignalInvalid, nil, nil
	}
	name := signalMemoName(token.Name(), key)
	var memos map[string]*TaskValue
	if task.Execution != nil {
		if task.Execution.Native != nil {
			memos = task.Execution.Native.Memos
		} else if task.Execution.Builtin != nil {
			memos = task.Execution.Builtin.Memos
		}
	}
	if memo := memos[name]; memo != nil {
		if generationObject(memo.Value)["expired"] == true {
			return SignalClosed, nil, nil
		}
		return SignalDuplicate, nil, nil
	}
	memos = maps.Clone(memos)
	if memos == nil {
		memos = make(map[string]*TaskValue, 1)
	}
	result := SignalDelivered
	memo := JSON{"version": token.Version(), "value": owned}
	if task.Execution != nil && task.Execution.Native != nil {
		state := task.Execution.Native.State
		if state.Status == "sleeping" && state.Until != nil && slices.Contains(state.Signals, SignalKey{Signal: token.Name(), Key: key}) && *state.Until <= t.session.now() {
			memo = JSON{"expired": true}
			result = SignalClosed
		}
	}
	memos[name] = &TaskValue{Present: true, Value: memo}
	if task.Execution != nil && task.Execution.Native != nil {
		native := *task.Execution.Native
		native.Memos = memos
		task.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &native}
	} else {
		builtin := BuiltinTaskExecution{}
		if task.Execution != nil && task.Execution.Builtin != nil {
			builtin = *task.Execution.Builtin
		}
		builtin.Memos = memos
		task.Execution = &TaskExecution{Tag: builtinTaskTag, Builtin: &builtin}
	}
	return result, &task, nil
}
