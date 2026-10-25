package goai

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type telemetryRecord struct {
	starts                          []string
	startAttrs, endAttrs            map[string]any
	events                          []map[string]any
	counters                        []string
	histogramAttrs                  []map[string]any
	status, errorName, errorMessage string
	ends                            int
}

func newTelemetryRecord() (*telemetryRecord, *TelemetrySpan) {
	r := &telemetryRecord{}
	child := &TelemetrySpan{SetAttributes: func(a map[string]any) { r.endAttrs = a }, AddEvent: func(n string, a map[string]any) { a["event"] = n; r.events = append(r.events, a) }, SetStatus: func(s, n, m string) { r.status, r.errorName, r.errorMessage = s, n, m }, End: func() { r.ends++ }}
	parent := &TelemetrySpan{StartSpan: func(n string, a map[string]any) *TelemetrySpan {
		r.starts = append(r.starts, n)
		r.startAttrs = a
		return child
	}, Metrics: &TelemetryMetrics{Counter: func(n string, v float64, a map[string]any) { r.counters = append(r.counters, n) }, Histogram: func(n string, v float64, a map[string]any, s *TelemetrySpan) {
		r.histogramAttrs = append(r.histogramAttrs, a)
	}}}
	return r, parent
}
func telemetryProvider(t *testing.T, events []Event) *Model {
	t.Helper()
	api := Api("telemetry-" + t.Name())
	stream := func(context.Context, *Model, *Context, *StreamOptions) <-chan Event {
		ch := make(chan Event, len(events))
		for _, e := range events {
			ch <- e
		}
		close(ch)
		return ch
	}
	RegisterApi(&ApiProvider{Api: api, Stream: stream, FetchDeferred: func(ctx context.Context, m *Model, h DeferredHandle, o *StreamOptions) <-chan Event {
		return stream(ctx, m, nil, o)
	}})
	t.Cleanup(func() { UnregisterApi(api) })
	return &Model{ID: "model", Provider: "provider", Api: api}
}
func TestTelemetry140RequestMetadataAndContentPrivacy(t *testing.T) {
	r, span := newTelemetryRecord()
	terminal := &Message{StopReason: StopReasonStop, ResponseID: "response", Usage: &Usage{Input: 10, Output: 2, CacheRead: 3, CacheWrite: 4, Cost: CostBreakdown{Total: .25}}}
	events := []Event{&StartEvent{}, &TextEndEvent{ContentIndex: 0, Content: "private😀"}, &ThinkingEndEvent{ContentIndex: 1, Content: "secret"}, &ToolCallDeltaEvent{ContentIndex: 2, Delta: "{\"private\":\"😀\"}"}, &ToolCallEndEvent{ContentIndex: 2, ToolCall: ToolCall{Name: "tool", Arguments: map[string]any{"private": "secret"}}}, &DoneEvent{Message: terminal}}
	model := telemetryProvider(t, events)
	var seen []Event
	for e := range Stream(context.Background(), model, &Context{SystemPrompt: "hidden prompt"}, &StreamOptions{Telemetry: span}) {
		seen = append(seen, e)
	}
	if !reflect.DeepEqual(events, seen) || r.ends != 1 || len(r.starts) != 1 || r.starts[0] != "pi.ai.request" {
		t.Fatalf("events=%v record=%+v", seen, r)
	}
	if r.endAttrs["usage.input"] != 10 || r.endAttrs["response.id"] != "response" || len(r.counters) != 5 || len(r.histogramAttrs) != 2 {
		t.Fatalf("%+v", r)
	}
	if r.events[1]["chars"] != 9 || r.events[2]["chars"] != 6 || r.events[3]["tool"] != "tool" {
		t.Fatal(r.events)
	}
	data, _ := json.Marshal([]any{r.startAttrs, r.endAttrs, r.events, r.histogramAttrs})
	for _, secret := range []string{"private", "secret", "hidden prompt"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("content leaked: %s", data)
		}
	}
}
func TestTelemetry140PollCountingAndErrorBounds(t *testing.T) {
	for _, stop := range []StopReason{StopReasonPending, StopReasonDeferred, StopReasonStop, StopReasonError, StopReasonAborted} {
		t.Run(string(stop), func(t *testing.T) {
			r, span := newTelemetryRecord()
			message := &Message{StopReason: stop, ErrorMessage: strings.Repeat("😀", 300), Usage: &Usage{Input: 10}}
			model := telemetryProvider(t, []Event{&DoneEvent{Message: message}})
			got, err := FetchDeferred(context.Background(), model, DeferredHandle{}, &StreamOptions{Telemetry: span})
			if err != nil || got != message || r.ends != 1 || r.starts[0] != "pi.ai.poll" {
				t.Fatalf("%v %v %+v", got, err, r)
			}
			want := 5
			if stop == StopReasonPending || stop == StopReasonDeferred {
				want = 0
			}
			if len(r.counters) != want {
				t.Fatal(r.counters)
			}
			if stop == StopReasonError {
				if r.status != "error" || r.errorName != "provider_error" || utf16Length(r.errorMessage) != 500 {
					t.Fatalf("%+v", r)
				}
			} else if r.status != "" {
				t.Fatalf("non-error outcome set error status: %+v", r)
			}
		})
	}
}
func TestTelemetry140BackendPanicsAndNoOptIn(t *testing.T) {
	message := &Message{StopReason: StopReasonStop}
	model := telemetryProvider(t, []Event{&DoneEvent{Message: message}})
	opts := &StreamOptions{Telemetry: &TelemetrySpan{StartSpan: func(string, map[string]any) *TelemetrySpan { panic("backend") }, Metrics: &TelemetryMetrics{Histogram: func(string, float64, map[string]any, *TelemetrySpan) { panic("backend") }, Counter: func(string, float64, map[string]any) { panic("backend") }}}}
	got, err := Complete(context.Background(), model, nil, opts)
	if err != nil || got != message {
		t.Fatal(got, err)
	}
	opts.Telemetry.StartSpan = func(string, map[string]any) *TelemetrySpan {
		return &TelemetrySpan{
			SetAttributes: func(map[string]any) { panic("backend") },
			AddEvent:      func(string, map[string]any) { panic("backend") },
			SetStatus:     func(string, string, string) { panic("backend") },
			End:           func() { panic("backend") },
		}
	}
	message.StopReason = StopReasonError
	got, err = Complete(context.Background(), model, nil, opts)
	if err != nil || got != message {
		t.Fatal(got, err)
	}
	source := make(chan Event)
	if observeRequestEvents(source, nil) != source {
		t.Fatal("no-op telemetry changed channel")
	}
	close(source)
	data, err := json.Marshal(opts)
	if err != nil || strings.Contains(string(data), "telemetry") {
		t.Fatal(string(data), err)
	}
}
func TestTelemetry140CancellationJoinsSpan(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	api := Api("telemetry-cancel")
	r, span := newTelemetryRecord()
	RegisterApi(&ApiProvider{Api: api, Stream: func(ctx context.Context, _ *Model, _ *Context, _ *StreamOptions) <-chan Event {
		ch := make(chan Event)
		go func() {
			defer close(ch)
			<-ctx.Done()
			ch <- &ErrorEvent{Reason: StopReasonAborted, Error: &Message{StopReason: StopReasonAborted}}
		}()
		return ch
	}})
	defer UnregisterApi(api)
	events := Stream(ctx, &Model{ID: "cancel", Api: api}, nil, &StreamOptions{Telemetry: span})
	cancel()
	for range events {
	}
	if r.ends != 1 || r.status != "" {
		t.Fatalf("%+v", r)
	}
}
