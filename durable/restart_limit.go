package durable

import (
	"context"
	"reflect"
)

const DefaultRestartLimit uint64 = 5

// RestartLimit reads the live host setting. Zero/nil follows the reference
// default. Go's maximum uint64 cap is the native effectively-unlimited mode.
func (h *Harness) RestartLimit() uint64 {
	settings := h.hostSettings.Load()
	if settings == nil {
		settings = h.options.Settings
	}
	if settings != nil && settings.RestartLimit != nil && *settings.RestartLimit != 0 {
		return *settings.RestartLimit
	}
	return DefaultRestartLimit
}
func (s *taskScheduler) parked(task Task) bool {
	return taskManaged(task) && task.Status != "running" && !terminalStatus(task.Status) && !taskHasDecidedOutcome(task) && !taskAborted(task) && task.Restarts >= s.h.RestartLimit()
}

// RetryTask clears a parked task's interruption count and enables scheduling.
// Unknown tasks reject; terminal, completing and non-parked tasks are no-ops.
func (h *Harness) RetryTask(ctx context.Context, id ID) error {
	if h.closing.Load() {
		return ErrClosed
	}
	_, err := h.session.taskCommit(ctx, func(tx *Tx) error {
		task, ok := tx.state.Tasks[id]
		if !ok {
			return reject("unknown task")
		}
		if !h.scheduler.parked(task) {
			return nil
		}
		task.Restarts = 0
		return tx.stage(Write{Op: "put-task", Task: &task})
	})
	if err == nil {
		h.scheduler.enable()
	}
	return err
}

// Only an invocation's own state transition can reset its count. Memos,
// identical intent checkpoints, tool presentation, registry migrations and
// reservation writes do not count as progress. JSON comparison is numeric, not spelling/byte equality.
func taskMadeProgress(before, after Task) bool {
	if terminalStatus(after.Status) || taskHasDecidedOutcome(after) {
		return true
	}
	if before.Execution != nil && before.Execution.Native != nil && after.Execution != nil && after.Execution.Native != nil {
		a, b := before.Execution.Native.State, after.Execution.Native.State
		return a.Status != b.Status || a.Policy != b.Policy || !reflect.DeepEqual(a.On, b.On) || !equalDeltaJSON(map[string]any(a.Checkpoint), map[string]any(b.Checkpoint))
	}
	if builtinToolCheckpoint(before) && builtinToolCheckpoint(after) {
		// Native tool checkpoints also carry recoverable presentation. Updating
		// or clearing that presentation is not an execution-state transition.
		return before.Status != after.Status || !equalToolCheckpointState(before.Checkpoint, after.Checkpoint)
	}
	return before.Status != after.Status || !equalDeltaJSON(map[string]any(before.Checkpoint), map[string]any(after.Checkpoint))
}

func builtinToolCheckpoint(task Task) bool {
	return task.Kind == "pi.tool" && (task.Execution == nil || task.Execution.Tag == builtinTaskTag && task.Execution.Builtin != nil)
}

func toolPresentationField(key string) bool {
	switch key {
	case "durationMs", "output", "details", "hasDetails", "droppedBytes", "droppedLines", "outputRaw", "outputFull", "outputBytes", "outputNewlines", "outputTerminated", "diagnostics":
		return true
	}
	return false
}

// Keep unknown fields significant; only the builtin tool's known presentation
// fields are excluded, without copying checkpoints or mutating caller maps.
func equalToolCheckpointState(a, b JSON) bool {
	for key, value := range a {
		if toolPresentationField(key) {
			continue
		}
		other, exists := b[key]
		if !exists || !equalDeltaJSON(value, other) {
			return false
		}
	}
	for key := range b {
		if !toolPresentationField(key) {
			if _, exists := a[key]; !exists {
				return false
			}
		}
	}
	return true
}
