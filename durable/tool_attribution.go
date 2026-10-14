package durable

import "context"

// ToolInvocation describes the actual task/caller boundary of a tool hook.
// Nested ParentTask identifies its caller, and Key is stable across replay.
// Model calls retain their transcript call ID and generation parent.
type ToolInvocation struct {
	Task       ID
	ParentTask ID
	CallID     string
	Key        string
	Caller     string
}

func (api *HookAPI) ToolInvocation(ctx context.Context) (ToolInvocation, error) {
	if api == nil || api.TaskRuntime == nil {
		return ToolInvocation{}, ErrSealed
	}
	r := api.TaskRuntime
	var out ToolInvocation
	err := r.harness.session.readTasks(ctx, func(state Snapshot) error {
		if err := r.check(); err != nil {
			return err
		}
		task, ok := state.Tasks[r.taskID]
		if !ok {
			return ErrSealed
		}
		out.Task = task.ID
		out.ParentTask = task.Owner
		switch task.Kind {
		case nestedToolKind:
			record, err := CanonicalTask(task, r.harness.session.limits)
			if err != nil {
				return err
			}
			input, err := nestedInput(record, r.harness.session.limits)
			if err != nil {
				return err
			}
			out.Caller = "tools"
			out.Key = input.Key
			out.CallID = input.Key
		case "pi.tool":
			var cp toolCheckpoint
			if err := fromObject(task.Checkpoint, &cp, r.harness.session.limits); err != nil {
				return err
			}
			out.Caller = "model"
			out.CallID = cp.CallID
		default:
			return reject("hook is not executing a tool")
		}
		return nil
	})
	return out, err
}
