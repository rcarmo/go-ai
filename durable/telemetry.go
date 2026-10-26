package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// DurableTelemetrySchemaVersion identifies the durable span, event and metric schema.
const DurableTelemetrySchemaVersion = 1

type telemetryContextKey struct{}

// TelemetryFromContext returns the current process-local durable phase span.
// Nil means telemetry is disabled. Spans never enter durable record JSON.
func TelemetryFromContext(ctx context.Context) *goai.TelemetrySpan {
	if ctx == nil {
		return nil
	}
	if carrier, ok := ctx.Value(telemetryContextKey{}).(*invocationTelemetry); ok {
		return carrier.span.Load()
	}
	return nil
}
func telemetryCall(fn func()) { defer func() { _ = recover() }(); fn() }
func telemetryDiagnostic(text string) string {
	units := 0
	for index, r := range text {
		n := 1
		if r > 0xffff {
			n = 2
		}
		if units+n > 500 {
			return text[:index]
		}
		units += n
	}
	return text
}

type phaseTraceState struct{ outcome, message string }

func taskTraceState(task Task) phaseTraceState {
	var outcome *TaskOutcome
	status := task.Status
	phase, _ := task.Checkpoint["phase"].(string)
	if task.Execution != nil {
		if native := task.Execution.Native; native != nil {
			status = native.State.Status
			outcome = native.State.Outcome
			phase, _ = native.State.Checkpoint["phase"].(string)
		}
		if builtin := task.Execution.Builtin; builtin != nil && builtin.Hold != nil {
			outcome = &builtin.Hold.Outcome
		}
	}
	if outcome != nil {
		value := outcome.Status
		if value == "completed" {
			value = "done"
		}
		message := ""
		if outcome.Error != nil {
			message = outcome.Error.Message
		}
		return phaseTraceState{value, message}
	}
	switch status {
	case "waiting", "sleeping":
		return phaseTraceState{outcome: status}
	case "pending", "running":
		return phaseTraceState{outcome: "next=" + phase}
	case "done":
		return phaseTraceState{outcome: "done"}
	case "failed":
		return phaseTraceState{outcome: "failed"}
	case "aborted":
		return phaseTraceState{outcome: "aborted"}
	}
	return phaseTraceState{outcome: "interrupted"}
}

type durablePhaseTrace struct {
	started time.Time
	span    *goai.TelemetrySpan
	phase   string
	state   phaseTraceState
}
type invocationTelemetry struct {
	root  *goai.TelemetrySpan
	span  atomic.Pointer[goai.TelemetrySpan]
	mu    sync.Mutex // protects adopted scalar observations, never held across host code
	phase *durablePhaseTrace
}

func (r *TaskRuntime) initTelemetry() {
	if r.harness.options.Telemetry == nil {
		return
	}
	r.telemetry = &invocationTelemetry{root: r.harness.options.Telemetry}
	r.context = context.WithValue(r.context, telemetryContextKey{}, r.telemetry)
}
func (r *TaskRuntime) beginPhase(checkpoint JSON) {
	trace := r.telemetry
	if trace == nil {
		return
	}
	phase, _ := checkpoint["phase"].(string)
	kind := "run"
	if r.abortMode {
		kind = "abort"
	}
	r.harness.mu.Lock()
	resumed := r.harness.telemetryLoaded[r.taskID]
	delete(r.harness.telemetryLoaded, r.taskID)
	r.harness.mu.Unlock()
	attrs := map[string]any{"task.id": uint64(r.taskID), "task.name": r.kind, "phase": phase, "kind": kind, "resumed": resumed}
	if attempt, ok := checkpoint["attempt"]; ok {
		if number, ok := exactNumber(attempt); ok && number.IsInt() && number.Num().IsInt64() {
			attrs["attempt"] = number.Num().Int64()
		}
	}
	span := &goai.TelemetrySpan{Metrics: trace.root.Metrics}
	if trace.root.StartSpan != nil {
		telemetryCall(func() {
			if child := trace.root.StartSpan("pi.durable.phase", attrs); child != nil {
				copy := *child
				if copy.Metrics == nil {
					copy.Metrics = trace.root.Metrics
				}
				span = &copy
			}
		})
	}
	trace.mu.Lock()
	trace.phase = &durablePhaseTrace{started: time.Now(), span: span, phase: phase, state: phaseTraceState{outcome: "interrupted"}}
	trace.mu.Unlock()
	trace.span.Store(span)
}

// Called on the Session line only after successful adoption. No backend code or
// record pointers escape; end callbacks run on the retained invocation host.
func (r *TaskRuntime) noteTrace(task Task) {
	if trace := r.telemetry; trace != nil {
		state := taskTraceState(task)
		trace.mu.Lock()
		if trace.phase != nil {
			trace.phase.state = state
		}
		trace.mu.Unlock()
	}
}
func (r *TaskRuntime) finishPhase() {
	trace := r.telemetry
	if trace == nil {
		return
	}
	trace.mu.Lock()
	phase := trace.phase
	trace.phase = nil
	trace.mu.Unlock()
	if phase == nil {
		return
	}
	trace.span.Store(nil)
	outcome := phase.state.outcome
	if phase.span.SetAttributes != nil {
		telemetryCall(func() { phase.span.SetAttributes(map[string]any{"outcome": outcome}) })
	}
	if phase.span.SetStatus != nil && (outcome == "faulted" || outcome == "failed" || outcome == "orphaned") {
		telemetryCall(func() { phase.span.SetStatus("error", "task_"+outcome, telemetryDiagnostic(phase.state.message)) })
	}
	if metrics := trace.root.Metrics; metrics != nil && metrics.Histogram != nil {
		telemetryCall(func() {
			metrics.Histogram("pi.durable.phase.duration", float64(time.Since(phase.started))/float64(time.Millisecond), map[string]any{"task": r.kind, "phase": phase.phase, "outcome": outcome}, phase.span)
		})
	}
	if phase.span.End != nil {
		telemetryCall(phase.span.End)
	}
}
func (h *Harness) reportTelemetry(ctx context.Context, info ReportInfo) {
	root := h.options.Telemetry
	if root == nil {
		return
	}
	span := TelemetryFromContext(ctx)
	if span == nil {
		span = root
	}
	if span.AddEvent != nil {
		attrs := map[string]any{"type": info.Type}
		if info.Reason != "" {
			attrs["reason"] = info.Reason
		}
		if info.Extension != "" {
			attrs["extension"] = info.Extension
		}
		if info.Task != nil {
			attrs["task.id"] = uint64(info.Task.ID)
		}
		telemetryCall(func() { span.AddEvent("pi.durable.report", attrs) })
	}
	if metrics := root.Metrics; metrics != nil && metrics.Counter != nil {
		telemetryCall(func() { metrics.Counter("pi.durable.reports", 1, map[string]any{"type": info.Type}) })
	}
}
