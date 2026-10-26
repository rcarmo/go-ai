package durable

import "time"

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
