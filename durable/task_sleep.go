package durable

import "time"

// Builtin clocks historically accept the full signed millisecond range, while
// the public native sleep state requires a nonnegative safe-integer deadline.
// Preserve that out-of-range behavior as an invocation wait; ordinary epoch
// deadlines use durable sleep and release the host permit after return.
func pacedTaskState(checkpoint JSON, until int64) *TaskState {
	if until < 0 || uint64(until) > MaxID {
		return &TaskState{Status: "running", Checkpoint: checkpoint}
	}
	return &TaskState{Status: "sleeping", Checkpoint: checkpoint, Until: &until}
}

func sleepWakeDelay(now, until int64) time.Duration {
	if until <= now {
		return 0
	}
	distance := uint64(until) - uint64(now)
	const maxDelay = uint64(1<<31 - 1)
	if distance > maxDelay {
		distance = maxDelay
	}
	return time.Duration(distance) * time.Millisecond
}
