package durable

import (
	"context"
	"time"
)

// A bad process-local clock must not unwind through a storage adoption. Match
// the reference wall-clock fallback; contain its diagnostic to one per Session
// so observational reporter reentry cannot create an unbounded feedback loop.
func callClock(now func() int64) (value int64, err error) {
	defer func() {
		if recover() != nil {
			value = time.Now().UnixMilli()
			err = reject("clock callback panic; using wall clock")
		}
	}()
	return now(), nil
}
func (h *Harness) readClock(runtime *TaskRuntime) int64 {
	if h.options.Now == nil {
		return time.Now().UnixMilli()
	}
	value, err := callClock(h.options.Now)
	if err != nil && h.session.clockFallbackReported.CompareAndSwap(false, true) {
		if runtime != nil {
			runtime.reportHostError(err, "now")
		} else {
			h.report(context.Background(), err, ReportInfo{Type: "host_error", Callback: "now"})
		}
	}
	return value
}
