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

// TelemetryFromContext returns the current process-local durable submit or phase span.
// Nil means telemetry is disabled. Spans never enter durable record JSON.
func TelemetryFromContext(ctx context.Context) *goai.TelemetrySpan {
	if ctx == nil {
		return nil
	}
	if trace := submitTraceFromContext(ctx); trace != nil {
		return trace.span
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
	parent := r.harness.telemetryParents[r.taskID] // Session-line owned at reservation
	if parent == nil {
		parent = r.harness.options.Telemetry
	}
	copy := *parent
	if copy.Metrics == nil {
		copy.Metrics = r.harness.options.Telemetry.Metrics
	}
	r.telemetry = &invocationTelemetry{root: &copy}
	r.context = context.WithValue(r.context, telemetryContextKey{}, r.telemetry)
}

// activeInvocation records signed deltas for the live host invocation, not each
// phase or durably parked task. Its paired calls run on the retained host, so
// Close joins the final decrement even when cancellation precedes phase entry.
func (r *TaskRuntime) activeInvocation(delta float64) {
	if root := r.harness.options.Telemetry; root != nil && root.Metrics != nil && root.Metrics.Counter != nil {
		telemetryCall(func() { root.Metrics.Counter("pi.durable.tasks.active", delta, map[string]any{"task": r.kind}) })
	}
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

// submitTrace contains only scalar admission observations. It is private to one
// submit call and is never stored in a record or used to alter admission.
type submitTelemetryContextKey struct{}
type submitTrace struct {
	span    *goai.TelemetrySpan
	results map[ID]string
	events  durableEventBuffer
}

func submitTraceFromContext(ctx context.Context) *submitTrace {
	if ctx == nil {
		return nil
	}
	trace, _ := ctx.Value(submitTelemetryContextKey{}).(*submitTrace)
	return trace
}
func (c *ConversationHandle) beginSubmitTrace(ctx context.Context, input Input) *submitTrace {
	parent := c.h.options.Telemetry
	kind := "input"
	if input.Type == "write" {
		kind = "write"
		if caller := TelemetryFromContext(ctx); caller != nil {
			parent = caller
		}
	}
	if parent == nil {
		return nil
	}
	attrs := map[string]any{"submission.type": kind, "conversation.id": uint64(c.id)}
	var span *goai.TelemetrySpan
	telemetryCall(func() {
		if kind == "input" && parent.StartRootSpan != nil {
			var links []*goai.TelemetrySpan
			if caller := TelemetryFromContext(ctx); caller != nil {
				links = []*goai.TelemetrySpan{caller}
			}
			span = parent.StartRootSpan("pi.durable.submit", attrs, links)
		} else if parent.StartSpan != nil {
			span = parent.StartSpan("pi.durable.submit", attrs)
		}
	})
	if span == nil {
		return nil
	}
	return &submitTrace{span: span}
}

// observe runs on the admission line; no backend callbacks, content, or retained
// record pointers escape. Multiple writes can include older settled submissions.
func (trace *submitTrace) observe(sub Submission) {
	result := "done"
	if sub.Status == "pending" || sub.Status == "running" {
		result = "queued"
		if submissionEntryID(sub.Value["placedEntry"]) != 0 {
			result = "placed"
		}
	}
	if trace.results == nil {
		trace.results = make(map[ID]string)
	}
	trace.results[sub.ID] = result
}

// finish runs after admission releases Harness/Session locks, including when
// validation, binding, or storage rejects the call. Error text may carry input.
func (trace *submitTrace) finish(handle *SubmissionHandle, err error) {
	trace.events.deliver()
	if err != nil {
		if trace.span.SetStatus != nil {
			telemetryCall(func() { trace.span.SetStatus("error", "", "") })
		}
	} else if handle != nil && trace.span.SetAttributes != nil {
		attrs := map[string]any{"submission.id": uint64(handle.id)}
		if result, ok := trace.results[handle.id]; ok {
			attrs["result"] = result
		}
		telemetryCall(func() { trace.span.SetAttributes(attrs) })
	}
	if trace.span.End != nil {
		telemetryCall(trace.span.End)
	}
}

// noteTaskAncestry runs only after Apply succeeds, on the Session line. A task
// inherits the creating scope once; later state writes cannot reparent it.
// Terminal tasks and Close release references without touching durable records.
func (h *Harness) noteTaskAncestry(ctx context.Context, tx *Tx, before Snapshot) {
	parent := TelemetryFromContext(ctx)
	if parent == nil {
		creator := tx.byTask
		if creator == 0 {
			creator = tx.progressTask
		}
		if runtime := h.scheduler.invocations[creator]; runtime != nil && runtime.telemetry != nil {
			parent = runtime.telemetry.span.Load()
		}
	}
	for _, write := range tx.writes {
		task := write.Task
		if task == nil {
			continue
		}
		if terminalStatus(task.Status) {
			delete(h.telemetryParents, task.ID)
			continue
		}
		if _, exists := before.Tasks[task.ID]; !exists && parent != nil {
			if h.telemetryParents == nil {
				h.telemetryParents = make(map[ID]*goai.TelemetrySpan)
			}
			h.telemetryParents[task.ID] = parent
		}
	}
}

// Event batches contain scalar attributes only, and live no longer than one
// synchronous admission/commit. Lock-owning public adapters defer delivery until
// their locks are released; other commits deliver after the Session line exits.
type telemetryEventsContextKey struct{}
type durableEvent struct {
	name  string
	attrs map[string]any
}
type durableEventBatch struct {
	span   *goai.TelemetrySpan
	events []durableEvent
	links  []*goai.TelemetrySpan
}
type durableEventBuffer struct {
	mu      sync.Mutex
	batches []*durableEventBatch
	closed  bool
}

func eventBufferFromContext(ctx context.Context) *durableEventBuffer {
	if ctx == nil {
		return nil
	}
	buffer, _ := ctx.Value(telemetryEventsContextKey{}).(*durableEventBuffer)
	return buffer
}
func (batch *durableEventBatch) deliver() {
	for _, link := range batch.links {
		telemetryCall(func() { batch.span.AddLink(link) })
	}
	for _, event := range batch.events {
		telemetryCall(func() { batch.span.AddEvent(event.name, event.attrs) })
	}
}
func (buffer *durableEventBuffer) enqueue(batch *durableEventBatch) {
	buffer.mu.Lock()
	closed := buffer.closed
	if !closed {
		buffer.batches = append(buffer.batches, batch)
	}
	buffer.mu.Unlock()
	if closed {
		batch.deliver()
	}
}
func (buffer *durableEventBuffer) deliver() {
	buffer.mu.Lock()
	batches := buffer.batches
	buffer.batches = nil
	buffer.closed = true
	buffer.mu.Unlock()
	for _, batch := range batches {
		batch.deliver()
	}
}
func (h *Harness) captureCommitEvents(ctx context.Context, tx *Tx, before Snapshot) *durableEventBatch {
	span := TelemetryFromContext(ctx)
	if span == nil {
		creator := tx.byTask
		if creator == 0 {
			creator = tx.progressTask
		}
		if runtime := h.scheduler.invocations[creator]; runtime != nil && runtime.telemetry != nil {
			span = runtime.telemetry.span.Load()
		}
	}
	if span == nil || (span.AddEvent == nil && span.AddLink == nil) {
		return nil
	}
	var batch *durableEventBatch
	if span.AddLink != nil {
		if links := h.queuedInputLinks(tx, before); len(links) != 0 {
			batch = &durableEventBatch{span: span, links: links}
		}
	}
	add := func(name string, attrs map[string]any) {
		if span.AddEvent == nil {
			return
		}
		if batch == nil {
			batch = &durableEventBatch{span: span}
		}
		batch.events = append(batch.events, durableEvent{name, attrs})
	}
	var seen map[ID]bool
	for _, write := range tx.writes {
		if entry := write.Entry; entry != nil {
			if _, existed := before.Entries[entry.ID]; !existed {
				add("pi.durable.entry", map[string]any{"entry.id": uint64(entry.ID), "entry.kind": entry.Kind})
			}
		}
		if sub := write.Submission; sub != nil {
			if seen[sub.ID] {
				continue
			}
			if seen == nil {
				seen = make(map[ID]bool)
			}
			seen[sub.ID] = true
			current := *sub
			if tx.taskAdoptState != nil {
				current = tx.taskAdoptState.Submissions[sub.ID]
			}
			if !terminalStatus(current.Status) {
				continue
			}
			if old, exists := before.Submissions[sub.ID]; exists && terminalStatus(old.Status) {
				continue
			}
			record := publicSubmission(current)
			attrs := map[string]any{"submission.id": uint64(sub.ID), "status": record.Status}
			if record.Answer != 0 {
				attrs["answer.entry_id"] = uint64(record.Answer)
			}
			add("pi.durable.submission", attrs)
		}
	}
	return batch
}

func (r *TaskRuntime) telemetryEvent(name string, attrs map[string]any) {
	if trace := r.telemetry; trace != nil {
		if span := trace.span.Load(); span != nil && span.AddEvent != nil {
			telemetryCall(func() { span.AddEvent(name, attrs) })
		}
	}
}
func (r *TaskRuntime) toolTelemetry(name, callID string) {
	if trace := r.telemetry; trace != nil {
		if span := trace.span.Load(); span != nil && span.SetAttributes != nil {
			telemetryCall(func() { span.SetAttributes(map[string]any{"tool.name": name, "tool.call_id": callID}) })
		}
	}
}

// Queued inputs already own placeholder generation tasks whose creating spans
// are their original submit roots. Capture those before adoption retires them.
// No transcript scan or persisted tracing metadata is needed.
func (h *Harness) queuedInputLinks(tx *Tx, before Snapshot) []*goai.TelemetrySpan {
	creator := tx.byTask
	if creator == 0 {
		creator = tx.progressTask
	}
	if creator == 0 {
		return nil
	}
	var links []*goai.TelemetrySpan
	var seen map[ID]bool
	var placeholders map[ID]*goai.TelemetrySpan
	for _, write := range tx.writes {
		task := write.Task
		if task == nil || task.ID == creator || task.Kind != "pi.generation" || !terminalStatus(task.Status) {
			continue
		}
		if parent := h.telemetryParents[task.ID]; parent != nil {
			if placeholders == nil {
				placeholders = make(map[ID]*goai.TelemetrySpan)
			}
			placeholders[submissionEntryID(task.Checkpoint["submission"])] = parent
		}
	}
	for _, write := range tx.writes {
		task := write.Task
		if task == nil || task.ID != creator || task.Kind != "pi.generation" {
			continue
		}
		merged, _ := task.Checkpoint["steered"].([]any)
		if len(merged) == 0 {
			continue
		}
		if seen == nil {
			seen = make(map[ID]bool)
			old, _ := before.Tasks[creator].Checkpoint["steered"].([]any)
			for _, value := range old {
				seen[submissionEntryID(value)] = true
			}
		}
		for _, value := range merged {
			id := submissionEntryID(value)
			if id == 0 || seen[id] {
				continue
			}
			seen[id] = true
			if parent := placeholders[id]; parent != nil {
				links = append(links, parent)
			}
		}
	}
	return links
}
