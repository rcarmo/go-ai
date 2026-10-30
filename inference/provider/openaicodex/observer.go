package openaicodex

import (
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

type codexObserverError struct {
	cause   error
	partial *goai.Message
}

func (e *codexObserverError) Error() string { return e.cause.Error() }
func (e *codexObserverError) Unwrap() error { return e.cause }

// Observe before Codex event normalization. Preserve exceptional code units
// only in declared tool-input text fields; ordinary metadata stays ordinary JSON.
func observeCodexEvent(raw []byte, opts *goai.StreamOptions, model *goai.Model) ([]byte, error) {
	if opts == nil || opts.OnProviderStreamEvent == nil {
		return raw, nil
	}
	var event map[string]interface{}
	if json.Unmarshal(raw, &event) != nil {
		return raw, nil
	}
	kind, ok := event["type"].(string)
	if !ok {
		return raw, nil
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	patch := func(target map[string]interface{}, encoded map[string]json.RawMessage, name string) {
		value, err := jsonparse.ParseJSONValue(string(encoded[name]), true)
		if err != nil {
			return
		}
		switch value.(type) {
		case string, goai.UTF16String:
			target[name] = value
		}
	}
	patchItem := func(item map[string]interface{}, encoded map[string]json.RawMessage) {
		switch item["type"] {
		case "function_call":
			patch(item, encoded, "arguments")
		case "custom_tool_call":
			patch(item, encoded, "input")
		}
	}
	switch kind {
	case "response.function_call_arguments.delta":
		patch(event, fields, "delta")
	case "response.function_call_arguments.done":
		patch(event, fields, "arguments")
	case "response.custom_tool_call_input.delta":
		patch(event, fields, "delta")
	case "response.custom_tool_call_input.done":
		patch(event, fields, "input")
	case "response.output_item.added", "response.output_item.done":
		if item, ok := event["item"].(map[string]interface{}); ok {
			var encoded map[string]json.RawMessage
			_ = json.Unmarshal(fields["item"], &encoded)
			patchItem(item, encoded)
		}
	case "response.created", "response.in_progress", "response.queued", "response.done", "response.completed", "response.incomplete", "response.failed", "response.cancelled":
		if response, ok := event["response"].(map[string]interface{}); ok {
			var encoded map[string]json.RawMessage
			_ = json.Unmarshal(fields["response"], &encoded)
			var output []json.RawMessage
			_ = json.Unmarshal(encoded["output"], &output)
			if items, ok := response["output"].([]interface{}); ok {
				for i, item := range items {
					if i >= len(output) {
						break
					}
					if tool, ok := item.(map[string]interface{}); ok {
						var encodedItem map[string]json.RawMessage
						_ = json.Unmarshal(output[i], &encodedItem)
						patchItem(tool, encodedItem)
					}
				}
			}
		}
	}
	if err := opts.OnProviderStreamEvent(event, model); err != nil {
		return nil, err
	}
	return json.Marshal(event)
}
