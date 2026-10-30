package anthropic

import (
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestAnthropicSSEArgumentCodeUnitsMain(t *testing.T) {
	source := "event: message_start\ndata: {\"message\":{\"id\":\"m\",\"usage\":{}}}\n\n" + "event: content_block_start\ndata: {\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"c\",\"name\":\"leaf\"}}\n\n" + `event: content_block_delta` + "\n" + `data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"{\"k\":\"\ud800"}}` + "\n\n" + "event: content_block_delta\n" + `data: {"index":0,"delta":{"type":"input_json_delta","partial_json":"\udc00\",\"l\":\"\udfff\"}"}}` + "\n\n" + "event: content_block_stop\ndata: {\"index\":0}\n\nevent: message_delta\ndata: {\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{}}\n\nevent: message_stop\ndata: {}\n\n"
	var deltas []string
	var final *goai.Message
	processAnthropicStream(strings.NewReader(source), &goai.Model{ID: "test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages}, nil, "", func(event goai.Event) {
		switch e := event.(type) {
		case *goai.ToolCallDeltaEvent:
			deltas = append(deltas, e.Delta)
		case *goai.DoneEvent:
			final = e.Message
		case *goai.ErrorEvent:
			t.Fatal(e.Err)
		}
	})
	if len(deltas) != 2 || final == nil || len(final.Content) != 1 {
		t.Fatal("missing tool deltas/final", len(deltas), final)
	}
	units, err := jsonparse.StringCodeUnits(deltas[0])
	if err != nil || units[len(units)-1] != 0xd800 {
		t.Fatal("first delta surrogate lost", units, err)
	}
	args := final.Content[0].Arguments
	if args["k"] != "𐀀" {
		t.Fatal("split pair not combined", args)
	}
	low, ok := args["l"].(goai.UTF16String)
	if !ok || !reflect.DeepEqual(low.CodeUnits(), []uint16{0xdfff}) {
		t.Fatal("isolated final unit lost", args)
	}
}
