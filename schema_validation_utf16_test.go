package goai

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rcarmo/go-ai/internal/jsonparse"
)

func TestSchemaToolArgumentsPreserveIsolatedUTF16Strings(t *testing.T) {
	for _, tc := range []struct {
		raw, pattern string
		length       int
		units        []uint16
		valid        bool
	}{
		{`"\ud800"`, "^.$", 1, []uint16{0xd800}, true},
		{`"\ud800"`, "^�$", 1, nil, false},
		{`"😀\ud800"`, "^..$", 2, []uint16{0xd83d, 0xde00, 0xd800}, true},
		{`"😀\ud800"`, "^😀�$", 2, nil, false},
		{`"😀\ud800"`, "^..$", 1, nil, false},
	} {
		scalar, err := jsonparse.ParseJSONValue(tc.raw, true)
		if err != nil {
			t.Fatal(err)
		}
		parameters, err := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string", "minLength": tc.length, "maxLength": tc.length, "pattern": tc.pattern}}, "required": []any{"text"}})
		if err != nil {
			t.Fatal(err)
		}
		call := ToolCall{Name: "unicode", Arguments: map[string]any{"text": scalar}}
		call.SetStreamingArguments(call.Arguments, "")
		got, err := ValidateToolArguments(&Tool{Name: "unicode", Parameters: parameters}, call)
		if (err == nil) != tc.valid {
			t.Error("UTF-16 schema result", tc.pattern, err)
		}
		if tc.valid {
			value, ok := got["text"].(UTF16String)
			if !ok || !reflect.DeepEqual(value.CodeUnits(), tc.units) {
				t.Error("UTF-16 schema identity", got)
			}
		}
	}
}

func TestSchemaToolArgumentsCheckFormatsForUTF16Strings(t *testing.T) {
	scalar, err := jsonparse.ParseJSONValue(`"\ud800"`, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		valid bool
	}{{"uuid", false}, {"regex", true}} {
		parameters, err := json.Marshal(map[string]any{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string", "format": tc.name}}})
		if err != nil {
			t.Fatal(err)
		}
		call := ToolCall{Name: "unicode", Arguments: map[string]any{"text": scalar}}
		call.SetStreamingArguments(call.Arguments, "")
		_, err = ValidateToolArguments(&Tool{Name: "unicode", Parameters: parameters}, call)
		if (err == nil) != tc.valid {
			t.Error("UTF-16 format result", tc.name, err)
		}
	}
}
