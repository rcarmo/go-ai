package goai

import "time"

// telemetryCall isolates optional backend failures from provider/cancellation
// semantics. Telemetry must never replace a provider outcome.
func telemetryCall(fn func()) { defer func() { _ = recover() }(); fn() }

func utf16Length(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r > 0xffff {
			n++
		}
	}
	return n
}
func boundedTelemetryError(s string) string {
	units := 0
	for i, r := range s {
		n := 1
		if r > 0xffff {
			n = 2
		}
		if units+n > 500 {
			return s[:i]
		}
		units += n
	}
	return s
}

type modelRequestObserver struct {
	started                      time.Time
	span                         *TelemetrySpan
	metrics                      *TelemetryMetrics
	provider, model              string
	poll, first, finished, ended bool
	toolChars                    map[int]int
}

func observeModelRequest(model *Model, opts *StreamOptions, poll bool) *modelRequestObserver {
	if model == nil || opts == nil || opts.Telemetry == nil {
		return nil
	}
	root := opts.Telemetry
	o := &modelRequestObserver{started: time.Now(), metrics: root.Metrics, provider: string(model.Provider), model: model.ID, poll: poll, first: true}
	if root.StartSpan != nil {
		attrs := o.attributes()
		attrs["api"] = string(model.Api)
		if opts.Reasoning != nil {
			attrs["reasoning"] = string(*opts.Reasoning)
		}
		name := "pi.ai.request"
		if poll {
			name = "pi.ai.poll"
		}
		telemetryCall(func() {
			if child := root.StartSpan(name, attrs); child != nil {
				o.span = child
			}
		})
	}
	if o.span == nil {
		o.span = &TelemetrySpan{}
	}
	return o
}
func (o *modelRequestObserver) attributes() map[string]any {
	return map[string]any{"provider": o.provider, "model": o.model}
}
func (o *modelRequestObserver) event(name string, attrs map[string]any) {
	if o.span.AddEvent != nil {
		telemetryCall(func() { o.span.AddEvent(name, attrs) })
	}
}
func (o *modelRequestObserver) histogram(name string, value float64, attrs map[string]any) {
	if o.metrics != nil && o.metrics.Histogram != nil {
		telemetryCall(func() { o.metrics.Histogram(name, value, attrs, o.span) })
	}
}
func (o *modelRequestObserver) counter(name string, value float64, attrs map[string]any) {
	if o.metrics != nil && o.metrics.Counter != nil {
		telemetryCall(func() { o.metrics.Counter(name, value, attrs) })
	}
}
func (o *modelRequestObserver) onEvent(event Event) {
	if o == nil {
		return
	}
	if o.first {
		o.first = false
		if o.span.AddEvent != nil {
			o.event("pi.ai.first_chunk", map[string]any{})
		}
		if o.metrics != nil && o.metrics.Histogram != nil {
			o.histogram("pi.ai.first_chunk.duration", float64(time.Since(o.started))/float64(time.Millisecond), o.attributes())
		}
	}
	switch e := event.(type) {
	case *TextEndEvent:
		if o.span.AddEvent == nil {
			return
		}
		o.event("pi.ai.block", map[string]any{"type": "text", "index": e.ContentIndex, "chars": utf16Length(e.Content)})
	case *ThinkingEndEvent:
		if o.span.AddEvent == nil {
			return
		}
		o.event("pi.ai.block", map[string]any{"type": "thinking", "index": e.ContentIndex, "chars": utf16Length(e.Content)})
	case *ToolCallDeltaEvent:
		if o.span.AddEvent == nil {
			return
		}
		if o.toolChars == nil {
			o.toolChars = map[int]int{}
		}
		o.toolChars[e.ContentIndex] += utf16Length(e.Delta)
	case *ToolCallEndEvent:
		if o.span.AddEvent == nil {
			return
		}
		chars := o.toolChars[e.ContentIndex]
		delete(o.toolChars, e.ContentIndex)
		o.event("pi.ai.block", map[string]any{"type": "tool_call", "index": e.ContentIndex, "chars": chars, "tool": e.ToolCall.Name})
	case *DoneEvent:
		o.onResult(e.Message)
	case *ErrorEvent:
		o.onResult(e.Error)
	}
}
func (o *modelRequestObserver) onResult(message *Message) {
	if o == nil || o.finished {
		return
	}
	o.finished = true
	if message == nil {
		return
	}
	usage := Usage{}
	if message.Usage != nil {
		usage = *message.Usage
	}
	if o.span.SetAttributes != nil {
		telemetryCall(func() {
			o.span.SetAttributes(map[string]any{"stop": string(message.StopReason), "usage.input": usage.Input, "usage.output": usage.Output, "usage.cache_read": usage.CacheRead, "usage.cache_write": usage.CacheWrite, "cost.total": usage.Cost.Total, "response.id": message.ResponseID})
		})
	}
	if message.StopReason == StopReasonError && o.span.SetStatus != nil {
		telemetryCall(func() { o.span.SetStatus("error", "provider_error", boundedTelemetryError(message.ErrorMessage)) })
	}
	if o.metrics != nil && o.metrics.Histogram != nil {
		attrs := o.attributes()
		attrs["stop"] = string(message.StopReason)
		o.histogram("pi.ai.request.duration", float64(time.Since(o.started))/float64(time.Millisecond), attrs)
	}
	if o.metrics != nil && o.metrics.Counter != nil && (!o.poll || (message.StopReason != StopReasonPending && message.StopReason != StopReasonDeferred)) {
		for _, v := range []struct {
			kind string
			n    int
		}{{"input", usage.Input}, {"output", usage.Output}, {"cache_read", usage.CacheRead}, {"cache_write", usage.CacheWrite}} {
			attrs := o.attributes()
			attrs["kind"] = v.kind
			o.counter("pi.ai.tokens", float64(v.n), attrs)
		}
		o.counter("pi.ai.cost", usage.Cost.Total, o.attributes())
	}
}
func (o *modelRequestObserver) end() {
	if o == nil || o.ended {
		return
	}
	o.ended = true
	if o.span.End != nil {
		telemetryCall(o.span.End)
	}
}

// Observe only when opted in. Forward the original events unchanged and retain
// Stream's documented obligation to drain the channel through provider close.
func observeRequestEvents(source <-chan Event, o *modelRequestObserver) <-chan Event {
	if o == nil {
		return source
	}
	out := make(chan Event)
	go func() {
		defer close(out)
		defer o.end()
		for event := range source {
			o.onEvent(event)
			out <- event
		}
	}()
	return out
}
