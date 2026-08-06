package bedrock

import (
	"errors"
	"reflect"
	"testing"
	"unsafe"

	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
)

type fakeConverseReader struct {
	events chan types.ConverseStreamOutput
	err    error
}

func (r *fakeConverseReader) Events() <-chan types.ConverseStreamOutput { return r.events }
func (r *fakeConverseReader) Close() error                              { return nil }
func (r *fakeConverseReader) Err() error                                { return r.err }

func setConverseEventStream(resp *bedrockruntime.ConverseStreamOutput, es *bedrockruntime.ConverseStreamEventStream) {
	v := reflect.ValueOf(resp).Elem().FieldByName("eventStream")
	reflect.NewAt(v.Type(), unsafe.Pointer(v.UnsafeAddr())).Elem().Set(reflect.ValueOf(es))
}

func TestProcessConverseStreamRejectsNonAssistantMessageStart(t *testing.T) {
	events := make(chan types.ConverseStreamOutput, 1)
	events <- &types.ConverseStreamOutputMemberMessageStart{Value: types.MessageStartEvent{Role: types.ConversationRoleUser}}
	close(events)
	resp := &bedrockruntime.ConverseStreamOutput{}
	stream := bedrockruntime.NewConverseStreamEventStream(func(es *bedrockruntime.ConverseStreamEventStream) {
		es.Reader = &fakeConverseReader{events: events}
	})
	setConverseEventStream(resp, stream)

	ch := make(chan goai.Event, 4)
	processConverseStream(resp, &goai.Model{ID: "m", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream}, ch)
	close(ch)

	for ev := range ch {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			if e.Err == nil || e.Err.Error() != "Unexpected assistant message start but got user message start instead" {
				t.Fatalf("unexpected role validation error: %#v", e)
			}
			return
		}
	}
	t.Fatal("expected ErrorEvent for non-assistant message start")
}

type bedrockNamedError struct{ name string }

func (e bedrockNamedError) Error() string     { return "stream broke" }
func (e bedrockNamedError) ErrorName() string { return e.name }

func TestProcessConverseStreamAddsFailureDiagnosticForStreamErr(t *testing.T) {
	events := make(chan types.ConverseStreamOutput, 1)
	events <- &types.ConverseStreamOutputMemberMessageStart{Value: types.MessageStartEvent{Role: types.ConversationRoleAssistant}}
	close(events)
	resp := &bedrockruntime.ConverseStreamOutput{}
	awsmiddleware.SetRequestIDMetadata(&resp.ResultMetadata, "request-id")
	stream := bedrockruntime.NewConverseStreamEventStream(func(es *bedrockruntime.ConverseStreamEventStream) {
		es.Reader = &fakeConverseReader{events: events, err: bedrockNamedError{name: "ModelStreamErrorException"}}
	})
	setConverseEventStream(resp, stream)

	ch := make(chan goai.Event, 4)
	processConverseStream(resp, &goai.Model{ID: "m", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream}, ch)
	close(ch)

	for ev := range ch {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			if len(e.Error.Diagnostics) != 1 || e.Error.Diagnostics[0].Type != "bedrock_response_failure" || e.Error.Diagnostics[0].Details["requestId"] != "request-id" || e.Error.Diagnostics[0].Details["errorCode"] != "ModelStreamErrorException" {
				t.Fatalf("unexpected diagnostic: %#v", e.Error.Diagnostics)
			}
			return
		}
	}
	t.Fatal("expected ErrorEvent from stream.Err")
}

func TestProcessConverseStreamSurfacesStreamErr(t *testing.T) {
	events := make(chan types.ConverseStreamOutput)
	close(events)
	resp := &bedrockruntime.ConverseStreamOutput{}
	stream := bedrockruntime.NewConverseStreamEventStream(func(es *bedrockruntime.ConverseStreamEventStream) {
		es.Reader = &fakeConverseReader{events: events, err: errors.New("stream broke")}
	})
	setConverseEventStream(resp, stream)

	ch := make(chan goai.Event, 4)
	processConverseStream(resp, &goai.Model{ID: "m", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream}, ch)
	close(ch)

	var sawErr bool
	for ev := range ch {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			sawErr = true
			if e.Err == nil || e.Err.Error() != "stream broke" {
				t.Fatalf("unexpected error: %#v", e)
			}
		}
	}
	if !sawErr {
		t.Fatal("expected ErrorEvent from stream.Err")
	}
}

func TestProcessConverseStreamPreservesRawStopReason(t *testing.T) {
	events := make(chan types.ConverseStreamOutput, 2)
	events <- &types.ConverseStreamOutputMemberMessageStart{Value: types.MessageStartEvent{Role: types.ConversationRoleAssistant}}
	events <- &types.ConverseStreamOutputMemberMessageStop{Value: types.MessageStopEvent{StopReason: types.StopReason("guardrail_intervened")}}
	close(events)
	resp := &bedrockruntime.ConverseStreamOutput{}
	stream := bedrockruntime.NewConverseStreamEventStream(func(es *bedrockruntime.ConverseStreamEventStream) {
		es.Reader = &fakeConverseReader{events: events}
	})
	setConverseEventStream(resp, stream)
	ch := make(chan goai.Event, 8)
	processConverseStream(resp, &goai.Model{ID: "m", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream}, ch)
	close(ch)
	for ev := range ch {
		if done, ok := ev.(*goai.DoneEvent); ok {
			if done.Message.RawStopReason != "guardrail_intervened" || done.Message.StopReason != goai.StopReasonError || done.Message.ErrorMessage != "Provider stopped with: guardrail_intervened" {
				t.Fatalf("message=%#v", done.Message)
			}
			return
		}
	}
	t.Fatal("missing done")
}

func TestMapStopReason(t *testing.T) {
	if got := mapStopReason(types.StopReasonToolUse); got != goai.StopReasonToolUse {
		t.Fatalf("expected toolUse, got %v", got)
	}
	if got := mapStopReason(types.StopReasonMaxTokens); got != goai.StopReasonLength {
		t.Fatalf("expected length, got %v", got)
	}
}
