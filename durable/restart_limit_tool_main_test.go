package durable

import (
	"encoding/json"
	"testing"
)

// Source302's output-only writes must not grant another restart budget. Native
// builtin checkpoints also retain presentation metadata for recovery; neither
// writing nor clearing it changes the execution state. Unknown fields remain
// conservative state changes, and public native checkpoints keep their rules.
func TestMainToolPresentationIsNotTaskStateProgress(t *testing.T) {
	base := Task{Kind: "pi.tool", Status: "running", Checkpoint: JSON{"started": true, "output": "", "arguments": map[string]any{"n": json.Number("1")}}, Execution: &TaskExecution{Tag: builtinTaskTag, Builtin: &BuiltinTaskExecution{}}}
	presentation := JSON{"durationMs": 12, "output": "line\n", "details": JSON{"step": 2}, "hasDetails": true, "droppedBytes": 5, "droppedLines": 1, "outputRaw": "retained", "outputFull": true, "outputBytes": 9, "outputNewlines": 1, "outputTerminated": true, "diagnostics": []any{JSON{"severity": "info", "message": "progress"}}}
	clone := func() Task {
		next := base
		next.Checkpoint = JSON{}
		for k, v := range base.Checkpoint {
			next.Checkpoint[k] = v
		}
		return next
	}
	for key, value := range presentation {
		t.Run(key, func(t *testing.T) {
			next := clone()
			next.Checkpoint[key] = value
			if taskMadeProgress(base, next) || taskMadeProgress(next, base) {
				t.Error("presentation write/removal resets restart count")
			}
		})
	}
	t.Run("combined", func(t *testing.T) {
		next := clone()
		for k, v := range presentation {
			next.Checkpoint[k] = v
		}
		if taskMadeProgress(base, next) || taskMadeProgress(next, base) {
			t.Error("combined presentation counted as progress")
		}
	})
	for _, key := range []string{"started", "arguments", "abort", "errorCode", "control", "result", "newStateField"} {
		t.Run("state-"+key, func(t *testing.T) {
			next := clone()
			next.Checkpoint[key] = "changed"
			if !taskMadeProgress(base, next) {
				t.Error("state transition lost restart reset")
			}
		})
	}
	t.Run("numeric-state-equality", func(t *testing.T) {
		next := clone()
		next.Checkpoint["arguments"] = map[string]any{"n": json.Number("1.0")}
		if taskMadeProgress(base, next) {
			t.Error("numeric spelling counted as progress")
		}
	})
	for _, status := range []string{"pending", "completing", "completed", "aborted"} {
		t.Run("status-"+status, func(t *testing.T) {
			next := clone()
			next.Status = status
			if !taskMadeProgress(base, next) {
				t.Error("status transition lost restart reset")
			}
		})
	}
	t.Run("non-tool-builtin", func(t *testing.T) {
		a, b := base, clone()
		a.Kind, b.Kind = "pi.generation", "pi.generation"
		b.Checkpoint["output"] = "changed"
		if !taskMadeProgress(a, b) {
			t.Error("non-tool state change ignored")
		}
	})
	t.Run("raw-builtin", func(t *testing.T) {
		a, b := base, clone()
		a.Execution, b.Execution = nil, nil
		b.Checkpoint["output"] = "changed"
		if taskMadeProgress(a, b) || taskMadeProgress(b, a) {
			t.Error("raw builtin presentation counted as state")
		}
	})
	t.Run("native", func(t *testing.T) {
		a, b := base, clone()
		a.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &NativeTaskExecution{State: TaskState{Status: "running", Checkpoint: JSON{"output": "old"}}}}
		b.Execution = &TaskExecution{Tag: nativeTaskTag, Native: &NativeTaskExecution{State: TaskState{Status: "running", Checkpoint: JSON{"output": "new"}}}}
		if !taskMadeProgress(a, b) {
			t.Error("native user state field ignored")
		}
	})
}
