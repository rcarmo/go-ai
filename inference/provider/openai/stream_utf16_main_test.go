package openai

import (
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestOpenAISSEArgumentCodeUnitsMain(t *testing.T) {
	source := `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c","function":{"name":"leaf","arguments":"{\"k\":\"\ud800"}}]}}]}` + "\n\n" + `data: {"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\udc00\",\"l\":\"\udfff\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" + "data: [DONE]\n\n"
	var deltas []string
	var final *goai.Message
	processSSEStream(strings.NewReader(source), &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions}, func(event goai.Event) {
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
