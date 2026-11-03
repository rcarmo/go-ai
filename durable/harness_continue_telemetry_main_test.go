package durable

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Selected f9a26365 harness-continue.test.ts:356-392 assertions. Native spans
// use callback/object identity rather than Chord context IDs; queued is the
// native preparation phase. This DOES NOT port the persisted status.trace
// assertion: native SubmissionRecord has no serializable trace context yet.
func TestMainContinueRootTraceAndAdmission(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		type spanRecord struct {
			name   string
			parent *spanRecord
			attrs  map[string]any
			links  []*goai.TelemetrySpan
			ended  bool
		}
		var mu sync.Mutex
		var spans []*spanRecord
		var create func(string, *spanRecord, map[string]any, []*goai.TelemetrySpan) *goai.TelemetrySpan
		create = func(name string, parent *spanRecord, attrs map[string]any, links []*goai.TelemetrySpan) *goai.TelemetrySpan {
			record := &spanRecord{name: name, parent: parent, attrs: map[string]any{}, links: append([]*goai.TelemetrySpan(nil), links...)}
			for key, value := range attrs {
				record.attrs[key] = value
			}
			mu.Lock()
			spans = append(spans, record)
			mu.Unlock()
			return &goai.TelemetrySpan{
				StartSpan: func(name string, attrs map[string]any) *goai.TelemetrySpan { return create(name, record, attrs, nil) },
				SetAttributes: func(attrs map[string]any) {
					mu.Lock()
					defer mu.Unlock()
					for key, value := range attrs {
						record.attrs[key] = value
					}
				},
				End: func() { mu.Lock(); record.ended = true; mu.Unlock() },
			}
		}
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if calls.Add(1) == 1 {
				ch <- mainContinueOwnedFailure()
			} else {
				ch <- terminal("recovered")
			}
			close(ch)
			return ch
		})
		options.Telemetry = &goai.TelemetrySpan{
			StartRootSpan: func(name string, attrs map[string]any, links []*goai.TelemetrySpan) *goai.TelemetrySpan {
				return create(name, nil, attrs, links)
			},
			StartSpan: func(name string, attrs map[string]any) *goai.TelemetrySpan { return create(name, nil, attrs, nil) },
		}
		h := openHarness(t, b.store, options)
		c := root(t, h, ref)
		first, err := c.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		if got := waitSubmission(t, first); got.Record.Status != "unanswered" || got.Record.Reason != "model_error" {
			t.Fatal(got)
		}
		caller := create("caller", nil, nil, nil)
		carrier := &invocationTelemetry{}
		carrier.span.Store(caller)
		ctx := context.WithValue(bg, telemetryContextKey{}, carrier)
		continued, err := c.Submit(ctx, Input{Type: "continue"})
		if err != nil {
			t.Fatal(err)
		}
		if got := waitSubmission(t, continued); got.Record.Status != "done" {
			t.Fatal(got)
		}
		caller.End()
		if err := h.WaitForIdle(bg); err != nil {
			t.Fatal(err)
		}
		// Join the native callback owners before inspecting all settled spans.
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		var submit *spanRecord
		for _, span := range spans {
			if !span.ended {
				t.Error("unsettled span", span.name)
			}
			if span.name == "pi.durable.submit" && span.attrs["submission.id"] == uint64(continued.ID()) {
				submit = span
			}
		}
		if submit == nil {
			t.Fatal("continue submit span missing")
		}
		if submit.parent != nil || submit.attrs["submission.type"] != "continue" || submit.attrs["result"] != "placed" || len(submit.links) != 1 || submit.links[0] != caller {
			t.Error("continue must be a placed root linked only to its caller", submit)
		}
		prepare := false
		for _, span := range spans {
			if span.name == "pi.durable.phase" && span.attrs["phase"] == "queued" && span.parent == submit {
				prepare = true
			}
		}
		if !prepare {
			t.Error("native preparation did not inherit the submit context")
		}
	})
}
