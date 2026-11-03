package durable

import (
	"errors"
	"fmt"
)

// v1.5.0 live.ts LIMITS_RESTARTS: these builtins settle their own cutoff,
// rather than acquiring the ordinary task's faulted outcome.
func selfLimitsRestarts(task Task) bool {
	return task.Kind == "pi.generation" || task.Kind == "pi.tool" || task.Kind == "task.pi.compaction"
}

func cutoffCount(n uint64) string {
	if n == 1 {
		return "once"
	}
	return fmt.Sprintf("%d times", n)
}
func (r *TaskRuntime) reportCutoff(restarts uint64, message string) {
	info := r.reportInfo("task_interrupted", "restart_limit")
	info.Restarts = &restarts
	r.report(errors.New(message), info)
}

func (h *Harness) endGenerationCutoff(r *TaskRuntime, task Task, cp generationCheckpoint) error {
	phase := cp.Phase
	switch phase {
	case "queued", "prepare-next":
		phase = "prepare"
	case "intent":
		phase = "request"
	case "compaction":
		phase = "compact"
	}
	count := cutoffCount(task.Restarts)
	if task.Restarts > 1 {
		count += " in a row"
	}
	message := fmt.Sprintf("The generation was interrupted %s in its %s phase", count, phase)
	var err error
	if cp.Phase == "finish" || cp.Phase == "drain" {
		err = h.endAfterRunWithQueue(r, task, cp, true)
	} else {
		if task.Restarts <= h.RestartLimit() {
			h.cancelDeferred(r.context, cp, r)
		}
		err = h.finishGenerationWithoutRequest(task, cp, "restart_limit", message)
	}
	if err == nil {
		r.reportCutoff(task.Restarts, message)
	}
	return err
}
