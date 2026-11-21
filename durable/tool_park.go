package durable

import (
	"context"
	"encoding/json"
	"fmt"
)

// ToolPark suspends the current call on a sleep or a wait. Exactly one must be present.
type ToolPark struct {
	Sleep *ToolSleep
	Wait  *ToolWait
}

// ToolSleep waits for a deadline, a signal, or whichever comes first.
type ToolSleep struct {
	Until         *int64
	Signals       []SignalKey
	Checkpoint    any
	HasCheckpoint bool
}

// ToolWait waits for owned tasks under the given task wait policy.
type ToolWait struct {
	On            []ID
	Policy        string
	Checkpoint    any
	HasCheckpoint bool
}

type toolResume struct {
	wake       *WakeReason
	checkpoint any
}

func resumedToolFromCheckpoint(cp JSON) toolResume {
	resume := toolResume{checkpoint: cp["checkpoint"]}
	if raw := cp["wake"]; raw != nil {
		if data, err := json.Marshal(raw); err == nil {
			_ = json.Unmarshal(data, &resume.wake)
		}
	}
	return resume
}

// ReadSignal reads a signal delivered to this tool's durable task.
func (a *ToolAPI) ReadSignal(ctx context.Context, token *SignalToken, key string) (*SignalState, error) {
	a.mu.Lock()
	runtime, active := a.runtime, a.active
	a.mu.Unlock()
	if !active {
		return nil, ErrSealed
	}
	if runtime == nil {
		return nil, ErrUnsupported
	}
	return runtime.ReadSignal(ctx, token, key)
}

func canonicalParkTool(ctx context.Context, r *TaskRuntime, api *ToolAPI, reg registeredTool, args any, park *ToolPark) error {
	if (park.Sleep == nil) == (park.Wait == nil) {
		return reject("tool park requires one sleep or wait")
	}
	replay := "unsafe"
	if reg.offer.ReplaySafe {
		replay = "safe"
	}
	cp := JSON{"phase": "parked", "arguments": args, "replay": replay}
	next := &TaskState{Checkpoint: cp}
	if sleep := park.Sleep; sleep != nil {
		next.Status, next.Until, next.Signals = "sleeping", sleep.Until, sleep.Signals
		if sleep.HasCheckpoint || sleep.Checkpoint != nil {
			cp["checkpoint"] = sleep.Checkpoint
		}
	} else {
		wait := park.Wait
		next.Status, next.On, next.Policy = "waiting", wait.On, wait.Policy
		if wait.HasCheckpoint || wait.Checkpoint != nil {
			cp["checkpoint"] = wait.Checkpoint
		}
	}
	progress, err := dtoObject(api.checkpoint, r.harness.session.limits)
	if err != nil {
		return err
	}
	return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		for _, id := range next.On {
			if task, found := tx.state.Tasks[id]; !found || task.Owner != r.taskID {
				return nil, fmt.Errorf("Task %d is not owned by tool task %d", id, r.taskID)
			}
		}
		if err := canonicalToolSlotUpdate(tx, r.taskID, r.conversation, func(slot JSON) {
			setToolPresentation(slot, progress, api.checkpoint.HasDetails)
			slot["status"] = next.Status
			for _, key := range []string{"output", "droppedBytes", "droppedLines"} {
				delete(slot, key)
			}
		}); err != nil {
			return nil, err
		}
		return next, nil
	})
}

func canonicalToolParkedPhase(ctx context.Context, task TaskRecord, r *TaskRuntime) error {
	input, call, err := readCanonicalTool(ctx, task, r)
	if err != nil {
		return err
	}
	agent, reg, found, err := canonicalToolSelection(ctx, r, input, call)
	if err != nil {
		return err
	}
	if task.Restarts >= r.harness.RestartLimit() {
		return canonicalToolCutOff(ctx, task, r, input, call, agent, reg)
	}
	if !found {
		message := fmt.Sprintf("Tool %s is not available", call.Name)
		r.reportError(fmt.Errorf("%s: its parked call %s cannot run again", message, call.ID))
		result, err := canonicalToolFromSlot(ctx, r, "tool_unavailable", message)
		if err != nil {
			return err
		}
		result, err = canonicalToolHooks(ctx, r, agent, call, result)
		if err != nil {
			return err
		}
		return settleCanonicalTool(ctx, r, input, call, reg, result, "failed", message, nil)
	}
	parked := taskCheckpointMap(task.State.Checkpoint)
	replay := "unsafe"
	if reg.offer.ReplaySafe {
		replay = "safe"
	}
	intent := JSON{"phase": "execute", "arguments": parked["arguments"], "replay": replay}
	if value, found := parked["checkpoint"]; found {
		intent["checkpoint"] = value
	}
	if wake := task.State.Wake; wake != nil {
		data, err := json.Marshal(wake)
		if err != nil {
			return err
		}
		var value any
		if err = decodePublicJSON(data, &value); err != nil {
			return err
		}
		intent["wake"] = value
	}
	if err := r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		if err := canonicalToolSlotUpdate(tx, r.taskID, r.conversation, func(slot JSON) { slot["status"] = "running"; delete(slot, "output") }); err != nil {
			return nil, err
		}
		return &TaskState{Status: "running", Checkpoint: intent}, nil
	}); err != nil {
		return err
	}
	return runCanonicalTool(ctx, r, input, call, agent, reg, unwrapToolArgumentValue(parked["arguments"]), toolResume{wake: task.State.Wake, checkpoint: parked["checkpoint"]})
}
