package bedrock

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
)

type lifecycleReader struct {
	events chan types.ConverseStreamOutput
	err    error
	closed atomic.Int32
}

func (r *lifecycleReader) Events() <-chan types.ConverseStreamOutput { return r.events }
func (r *lifecycleReader) Err() error                                { return r.err }
func (r *lifecycleReader) Close() error                              { r.closed.Add(1); return nil }
func lifecycleResponse(reader *lifecycleReader) *bedrockruntime.ConverseStreamOutput {
	response := &bedrockruntime.ConverseStreamOutput{}
	awsmiddleware.SetRequestIDMetadata(&response.ResultMetadata, "request-id")
	setConverseEventStream(response, bedrockruntime.NewConverseStreamEventStream(func(s *bedrockruntime.ConverseStreamEventStream) { s.Reader = reader }))
	return response
}

func TestBedrockRawHookLifecycleMain(t *testing.T) {
	for _, mode := range []string{"success", "hook-error", "cancel", "cancel-and-error", "bad-role", "empty", "stream-error"} {
		t.Run(mode, func(t *testing.T) {
			reader := &lifecycleReader{events: make(chan types.ConverseStreamOutput, 3)}
			if mode != "empty" {
				role := types.ConversationRoleAssistant
				if mode == "bad-role" {
					role = types.ConversationRoleUser
				}
				reader.events <- &types.ConverseStreamOutputMemberMessageStart{Value: types.MessageStartEvent{Role: role}}
				reader.events <- &types.ConverseStreamOutputMemberContentBlockDelta{Value: types.ContentBlockDeltaEvent{ContentBlockIndex: aws.Int32(0), Delta: &types.ContentBlockDeltaMemberText{Value: "prefix"}}}
				reader.events <- &types.ConverseStreamOutputMemberMessageStop{Value: types.MessageStopEvent{StopReason: types.StopReasonEndTurn}}
			}
			close(reader.events)
			sentinel := errors.New("observer refused stop")
			if mode == "stream-error" {
				reader.err = sentinel
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			observed, normalized, done := 0, 0, 0
			var terminal *goai.ErrorEvent
			model := &goai.Model{ID: "model", Api: goai.ApiBedrockConverseStream}
			diagnostic := goai.AssistantMessageDiagnostic{Type: "bedrock_strict_tools_fallback", Timestamp: 1}
			opts := &goai.StreamOptions{OnProviderStreamEvent: func(raw any, gotModel *goai.Model) error {
				if gotModel != model {
					t.Error("observer model changed")
				}
				observed++
				if _, ok := raw.(*types.ConverseStreamOutputMemberContentBlockDelta); ok && normalized != 0 {
					t.Error("raw event observed after normalization")
				}
				if _, ok := raw.(*types.ConverseStreamOutputMemberMessageStop); ok {
					if mode == "cancel" || mode == "cancel-and-error" {
						cancel()
					}
					if mode == "hook-error" || mode == "cancel-and-error" {
						return sentinel
					}
				}
				return nil
			}}
			processBedrockStream(ctx, lifecycleResponse(reader), model, opts, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.TextDeltaEvent:
					normalized++
				case *goai.DoneEvent:
					done++
				case *goai.ErrorEvent:
					if terminal != nil {
						t.Error("duplicate terminal error")
					}
					terminal = e
				}
			}, diagnostic)
			if reader.closed.Load() != 1 {
				t.Fatal("stream not closed exactly once", reader.closed.Load())
			}
			wantObserved := 3
			if mode == "empty" {
				wantObserved = 0
			}
			if mode == "bad-role" {
				wantObserved = 1
			}
			if observed != wantObserved {
				t.Fatal("raw event count", observed, wantObserved)
			}
			if mode == "success" {
				if terminal != nil || done != 1 {
					t.Fatal("success terminal", terminal, done)
				}
				return
			}
			if terminal == nil || done != 0 {
				t.Fatal("failure terminal", terminal, done)
			}
			want := goai.StopReasonError
			if mode == "cancel" || mode == "cancel-and-error" {
				want = goai.StopReasonAborted
			}
			if terminal.Reason != want || terminal.Error.StopReason != want {
				t.Fatal("terminal classification", terminal)
			}
			if mode == "hook-error" && !errors.Is(terminal.Err, sentinel) {
				t.Fatal("observer error identity lost")
			}
			if want == goai.StopReasonAborted && !errors.Is(terminal.Err, context.Canceled) {
				t.Fatal("cancellation identity lost")
			}
			if len(terminal.Error.Diagnostics) == 0 || terminal.Error.Diagnostics[0].Type != diagnostic.Type {
				t.Fatal("prior diagnostic lost", terminal.Error.Diagnostics)
			}
			if mode != "empty" && mode != "bad-role" && (len(terminal.Error.Content) != 1 || terminal.Error.Content[0].Text != "prefix") {
				t.Fatal("partial prefix lost", terminal.Error.Content)
			}
			if want == goai.StopReasonAborted && len(terminal.Error.Diagnostics) != 1 {
				t.Fatal("abort acquired failure diagnostic", terminal.Error.Diagnostics)
			}
		})
	}
}

func TestBedrockCancellationWhileWaitingMain(t *testing.T) {
	reader := &lifecycleReader{events: make(chan types.ConverseStreamOutput, 1)}
	reader.events <- &types.ConverseStreamOutputMemberMessageStart{Value: types.MessageStartEvent{Role: types.ConversationRoleAssistant}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	seen := make(chan struct{})
	finished := make(chan struct{})
	terminal := make(chan *goai.ErrorEvent, 1)
	go func() {
		defer close(finished)
		processBedrockStream(ctx, lifecycleResponse(reader), &goai.Model{ID: "model"}, &goai.StreamOptions{OnProviderStreamEvent: func(any, *goai.Model) error { close(seen); return nil }}, func(event goai.Event) {
			if e, ok := event.(*goai.ErrorEvent); ok {
				terminal <- e
			}
		})
	}()
	select {
	case <-seen:
	case <-time.After(time.Second):
		t.Fatal("observer did not receive start")
	}
	cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("cancellation waited for another provider event")
	}
	e := <-terminal
	if e.Reason != goai.StopReasonAborted || reader.closed.Load() != 1 {
		t.Fatal("cancelled stream not retired", e, reader.closed.Load())
	}
}

func TestBedrockRawResponseHeadersAndEventHookWireMain(t *testing.T) {
	for _, failHook := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "observer-failure"}[failHook], func(t *testing.T) {
			disconnected := make(chan struct{})
			var requests atomic.Int32
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
				w.Header().Set("X-Amzn-Requestid", "req-123")
				w.Header().Set("X-Bifrost-Provider", "bedrock")
				w.Header().Set("X-Bifrost-Resolved-Model", "model")
				encoder := eventstream.NewEncoder()
				frames := []struct{ name, payload string }{{"messageStart", `{"role":"assistant"}`}, {"messageStop", `{"stopReason":"end_turn"}`}}
				for _, f := range frames {
					var headers eventstream.Headers
					headers.Set(":message-type", eventstream.StringValue("event"))
					headers.Set(":event-type", eventstream.StringValue(f.name))
					if err := encoder.Encode(w, eventstream.Message{Headers: headers, Payload: []byte(f.payload)}); err != nil {
						t.Error(err)
					}
				}
				w.(http.Flusher).Flush()
				if failHook {
					select {
					case <-r.Context().Done():
						close(disconnected)
					case <-ctx.Done():
					}
				}
			}))
			defer server.Close()
			model := &goai.Model{ID: "model", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, BaseURL: server.URL}
			responses, raw, done := 0, 0, 0
			var terminal *goai.ErrorEvent
			sentinel := &types.ValidationException{Message: aws.String("The compiled grammar is too large")}
			supported := true
			model.BedrockCompat = &goai.BedrockCompat{SupportsStrictMode: &supported}
			opts := &goai.StreamOptions{Region: "us-east-1", Env: goai.ProviderEnv{"AWS_BEDROCK_SKIP_AUTH": "1", "AWS_EC2_METADATA_DISABLED": "true"}, OnResponse: func(status int, headers map[string]string, gotModel *goai.Model) {
				responses++
				if status != 200 || gotModel != model {
					t.Error("response hook metadata", status)
				}
				for key, want := range map[string]string{"X-Amzn-Requestid": "req-123", "X-Bifrost-Provider": "bedrock", "X-Bifrost-Resolved-Model": "model"} {
					if headers[key] != want {
						t.Error("raw response header lost", key, headers)
					}
				}
			}, OnProviderStreamEvent: func(any, *goai.Model) error {
				raw++
				if responses != 1 {
					t.Error("provider event before response hook")
				}
				if failHook {
					return sentinel
				}
				return nil
			}}
			for event := range streamBedrock(ctx, model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}, Tools: []goai.Tool{mainStrictTool("preferred", "prefer")}}, opts) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					done++
				case *goai.ErrorEvent:
					terminal = e
				}
			}
			if responses != 1 || requests.Load() != 1 {
				t.Fatal("response hook/request count", responses, requests.Load())
			}
			if failHook {
				if terminal == nil || !errors.Is(terminal.Err, sentinel) || raw != 1 || done != 0 {
					t.Fatal("observer failure", terminal, raw, done)
				}
				select {
				case <-disconnected:
				case <-time.After(time.Second):
					t.Fatal("observer failure did not close HTTP stream")
				}
			} else if terminal != nil || done != 1 || raw != 2 {
				t.Fatal("stream hook success", terminal, done, raw)
			}
		})
	}
}

func TestBedrockResponseHookWithoutRequestIDMain(t *testing.T) {
	called := 0
	invokeBedrockResponseHook(&goai.StreamOptions{OnResponse: func(status int, headers map[string]string, _ *goai.Model) {
		called++
		if status != 200 || !reflect.DeepEqual(headers, map[string]string{}) {
			t.Fatal("missing-header response hook", status, headers)
		}
	}}, &bedrockruntime.ConverseStreamOutput{}, &goai.Model{})
	if called != 1 {
		t.Fatal("response hook omitted")
	}
}
