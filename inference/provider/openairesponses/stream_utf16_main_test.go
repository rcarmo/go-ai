package openairesponses

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestResponsesSSEArgumentCodeUnitsMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiOpenAIResponses, goai.ApiAzureOpenAIResponses} {
		t.Run(string(api), func(t *testing.T) {
			source := `data: {"type":"response.output_item.added","item":{"type":"function_call","id":"i","call_id":"c","name":"leaf"}}` + "\n\n" + `data: {"type":"response.function_call_arguments.delta","delta":"{\"k\":\"\ud800"}` + "\n\n" + `data: {"type":"response.function_call_arguments.delta","delta":"\udc00\",\"l\":\""}` + "\n\n" + `data: {"type":"response.function_call_arguments.done","arguments":"{\"k\":\"\ud800\udc00\",\"l\":\"\udfff\"}"}` + "\n\n" + `data: {"type":"response.output_item.done","item":{"type":"function_call","arguments":"{\"k\":\"\ud800\udc00\",\"l\":\"\udfff\"}","namespace":"ns"}}` + "\n\n" + `data: {"type":"response.completed","response":{"status":"completed","usage":{}}}` + "\n\n"
			var deltas []*goai.ToolCallDeltaEvent
			var final *goai.Message
			var call *goai.ToolCall
			processStream(strings.NewReader(source), &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: api}, func(event goai.Event) {
				switch e := goai.SnapshotEvent(event).(type) {
				case *goai.ToolCallDeltaEvent:
					deltas = append(deltas, e)
				case *goai.ToolCallEndEvent:
					call = &e.ToolCall
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ErrorEvent:
					t.Fatal(e.Err)
				}
			})
			if len(deltas) != 3 || final == nil || call == nil {
				t.Fatal("missing deltas/final", len(deltas), final)
			}
			units, err := jsonparse.StringCodeUnits(deltas[0].Delta)
			if err != nil || units[len(units)-1] != 0xd800 {
				t.Fatal("delta high unit lost", units, err)
			}
			args := final.Content[0].Arguments
			if args["k"] != "𐀀" {
				t.Fatal("split pair not combined", args)
			}
			low, ok := args["l"].(goai.UTF16String)
			if !ok || !reflect.DeepEqual(low.CodeUnits(), []uint16{0xdfff}) {
				t.Fatal("low unit lost", args)
			}
			if !call.HasStreamingArguments() || !final.Content[0].HasStreamingArguments() || call.Namespace != "ns" {
				t.Fatal("final ownership/namespace lost", call)
			}
			if _, exists := deltas[0].Partial.Content[0].Arguments["l"]; exists {
				t.Fatal("earlier snapshot mutated")
			}
		})
	}
}

func TestResponsesSSEAuthoritativeArgumentsMain(t *testing.T) {
	for _, finalItem := range []bool{false, true} {
		t.Run(map[bool]string{false: "arguments_done", true: "item_done"}[finalItem], func(t *testing.T) {
			scalar, err := jsonparse.ParseJSONValue(`"\udfff"`, true)
			if err != nil {
				t.Fatal(err)
			}
			rawLow := scalar.(goai.UTF16String).WTF8()
			wire := func(value any) string {
				raw, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				return "data: " + string(raw) + "\n\n"
			}
			source := wire(map[string]any{"type": "response.output_item.added", "item": map[string]any{"type": "function_call", "id": "i", "call_id": "c", "name": "leaf"}})
			source += wire(map[string]any{"type": "response.function_call_arguments.delta", "delta": "{\"stale\":1}"})
			source += wire(map[string]any{"type": "response.function_call_arguments.done", "arguments": jsonparse.ArgumentText("{\"low\":\"" + rawLow + "\"}")})
			item := map[string]any{"type": "function_call"}
			if finalItem {
				item["arguments"] = jsonparse.ArgumentText("{\"final\":\"" + rawLow + "\"}")
			}
			source += wire(map[string]any{"type": "response.output_item.done", "item": item})
			source += wire(map[string]any{"type": "response.completed", "response": map[string]any{"status": "completed", "usage": map[string]any{}}})
			var final *goai.Message
			deltas := 0
			processStream(strings.NewReader(source), &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}, func(event goai.Event) {
				switch e := event.(type) {
				case *goai.DoneEvent:
					final = e.Message
				case *goai.ToolCallDeltaEvent:
					deltas++
				case *goai.ErrorEvent:
					t.Fatal(e.Err)
				}
			})
			if final == nil || deltas != 1 {
				t.Fatal("replacement emitted false continuation", deltas, final)
			}
			key := "low"
			if finalItem {
				key = "final"
			}
			args := final.Content[0].Arguments
			if len(args) != 1 {
				t.Fatal("stale arguments retained", args)
			}
			value, ok := args[key].(goai.UTF16String)
			if !ok || !reflect.DeepEqual(value.CodeUnits(), []uint16{0xdfff}) {
				t.Fatal("authoritative units lost", args)
			}
		})
	}
}
