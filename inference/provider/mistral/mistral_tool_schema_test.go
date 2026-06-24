package mistral

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMistralToolSchemaSerializationStripsNoSymbolKeysAndPreservesNestedSchema(t *testing.T) {
	params := json.RawMessage(`{"type":"object","properties":{"nested":{"type":"object","properties":{"value":{"type":"string"}}}}}`)
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("Hi")}, Tools: []goai.Tool{{Name: "inspect_schema", Description: "Inspect the schema", Parameters: params}}}
	payload := buildRequest(&goai.Model{ID: "devstral-medium-latest", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, ctx, nil)
	if len(payload.Tools) != 1 {
		t.Fatalf("tools=%#v", payload.Tools)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload.Tools[0].Function.Parameters, &decoded); err != nil {
		t.Fatal(err)
	}
	props, ok := decoded["properties"].(map[string]any)
	if !ok || props["nested"] == nil {
		t.Fatalf("properties=%#v", decoded["properties"])
	}
	if string(payload.Tools[0].Function.Parameters) != string(params) {
		t.Fatalf("parameters=%s want %s", payload.Tools[0].Function.Parameters, params)
	}
}
