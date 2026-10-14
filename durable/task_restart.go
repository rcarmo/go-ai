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
