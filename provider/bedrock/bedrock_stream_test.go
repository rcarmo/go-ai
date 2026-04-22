package bedrock

import (
	"errors"
	"reflect"
	"testing"
	"unsafe"

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

func TestMapStopReason(t *testing.T) {
	if got := mapStopReason(types.StopReasonToolUse); got != goai.StopReasonToolUse {
		t.Fatalf("expected toolUse, got %v", got)
	}
	if got := mapStopReason(types.StopReasonMaxTokens); got != goai.StopReasonLength {
		t.Fatalf("expected length, got %v", got)
	}
}
