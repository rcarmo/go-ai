package durable

// Restart reasons flow only from cancellation-intended ancestors. Explicit
// marks/noncompleted holds override restart so ordinary orphan cleanup can run.
func restartCascadeReason(state Snapshot, task Task) string {
	seen := map[ID]bool{}
	for owner := task.Owner; owner != 0 && !seen[owner]; {
		seen[owner] = true
		parent, ok := state.Tasks[owner]
		if !ok {
			return ""
		}
		if taskHasDecidedOutcome(parent) && taskFailedOutcome(parent) {
			return ""
		}
		if taskAborted(parent) {
			return parent.AbortReason
		}
		owner = parent.Owner
	}
	return ""
}

// A recovered execute phase must not publish an interruption over its parked
// children before their independent restart marks take effect. The caller's
// failed outcome would otherwise turn their restart into ordinary cancellation.
// An explicit abort still supersedes these marks, and parked callers can resume
// without waiting for children they retained.
func (s *taskScheduler) recoveredToolWaitsForParkedChildren(state Snapshot, task Task) bool {
	if task.Restarts == 0 || taskAborted(task) || !canonicalToolRecord(task) || taskCheckpointMap(task.Execution.Native.State.Checkpoint)["phase"] != "execute" {
		return false
	}
	for id, child := range state.Tasks {
		if child.Owner == task.ID && !terminalStatus(child.Status) && (s.abandoned[id] || child.AbortReason == "restart") && canonicalToolRecord(child) && taskCheckpointMap(child.Execution.Native.State.Checkpoint)["phase"] == "parked" {
			return true
		}
	}
	return false
}
