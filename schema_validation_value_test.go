package goai

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Pinned pi-ai validation.ts returns schema.parse(structuredClone(arguments)).
// An array schema wraps an object call; the result is not constrained to a map.
func TestSchemaToolArgumentsReturnCoercedArrayValue(t *testing.T) {
	original := map[string]any{"n": "2"}
	tool := Tool{Name: "array", Parameters: json.RawMessage(`{"type":"array","items":{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}}`)}
	got, err := ValidateToolArguments(&tool, ToolCall{Name: tool.Name, Arguments: original})
	if err != nil {
		t.Fatal("source array value rejected", err)
	}
	encoded, err := json.Marshal(got)
	if err != nil || string(encoded) != `[{"n":2}]` {
		t.Fatal("source array value", got, err)
	}
	// The public result owns its coerced containers independently of the call.
	value := any(got).([]any)
	value[0].(map[string]any)["host-mutation"] = true
	if !reflect.DeepEqual(original, map[string]any{"n": "2"}) {
		t.Fatal("validation mutated original call", original)
	}
}

func TestToolCallCoercedArrayValueJSONRoundTrip(t *testing.T) {
	const wire = `{"type":"toolCall","id":"value","name":"array","arguments":[{"\ud800":"\ud801"}]}`
	var call ToolCall
	if err := json.Unmarshal([]byte(wire), &call); err != nil {
		t.Fatal("source array tool call decode", err)
	}
	array, ok := call.Arguments.([]any)
	if !ok || len(array) != 1 {
		t.Fatal("source array tool call shape", call.Arguments)
	}
	object, ok := array[0].(map[string]any)
	if !ok || len(object) != 1 {
		t.Fatal("source array tool call object", array[0])
	}
	for key, value := range object {
		if key != "\xed\xa0\x80" {
			t.Error("source array UTF-16 key", key)
		}
		scalar, ok := value.(UTF16String)
		if !ok || !reflect.DeepEqual(scalar.CodeUnits(), []uint16{0xd801}) {
			t.Error("source array UTF-16 scalar", value)
		}
	}
	encoded, err := json.Marshal(call)
	if err != nil || string(encoded) != wire {
		t.Fatal("source array tool call encode", string(encoded), err)
	}
}

func TestSchemaToolArgumentsDetachUnconstrainedDescendants(t *testing.T) {
	for _, parameters := range []string{`{}`, `{"type":"array"}`} {
		original := map[string]any{"free": map[string]any{"value": "original"}}
		got, err := ValidateToolArguments(&Tool{Name: "clone", Parameters: json.RawMessage(parameters)}, ToolCall{Name: "clone", Arguments: original})
		if err != nil {
			t.Fatal(err)
		}
		var object map[string]any
		if parameters == `{}` {
			object = got.(map[string]any)
		} else {
			object = got.([]any)[0].(map[string]any)
		}
		object["free"].(map[string]any)["value"] = "host mutation"
		if original["free"].(map[string]any)["value"] != "original" {
			t.Error("source structured clone aliases unconstrained child", parameters)
		}
	}
}
