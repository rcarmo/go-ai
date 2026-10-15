package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestTool130ArgumentErrorRejectsEvenWithoutSchema(t *testing.T) {
	call := goai.ToolCall{ID: "call", Name: "leaf", Arguments: map[string]interface{}{}, ArgumentsError: "invalid object\n[1]"}
	for _, schema := range []json.RawMessage{nil, json.RawMessage(`{"type":"object"}`)} {
		tool := goai.Tool{Name: "leaf", Parameters: schema}
		if _, err := goai.ValidateToolArguments(&tool, call); err == nil || !strings.Contains(err.Error(), call.ArgumentsError) {
			t.Fatal(err)
		}
	}
	block := goai.ContentBlock{Type: "toolCall", ID: call.ID, Name: call.Name, Arguments: call.Arguments, ArgumentsError: call.ArgumentsError}
	encoded, err := json.Marshal(block)
	if err != nil {
		t.Fatal(err)
	}
	var restored goai.ContentBlock
	if err = json.Unmarshal(encoded, &restored); err != nil || restored.ArgumentsError != call.ArgumentsError {
		t.Fatal(string(encoded), err)
	}
}
