package openairesponses

import goai "github.com/rcarmo/go-ai"

// Azure normalization only rewrites reasoning. Keep original tool-call
// JSON rather than reserializing its code units through an ordinary map.
func responsesToolArgumentEvent(event map[string]interface{}) bool {
	kind, _ := event["type"].(string)
	switch kind {
	case "response.function_call_arguments.delta", "response.function_call_arguments.done", "response.custom_tool_call_input.delta", "response.custom_tool_call_input.done":
		return true
	case "response.output_item.added", "response.output_item.done":
		item, _ := event["item"].(map[string]interface{})
		return item["type"] == "function_call" || item["type"] == "custom_tool_call"
	}
	return false
}
func responsesArgumentDelta(delta any) string {
	switch text := delta.(type) {
	case string:
		return text
	case goai.UTF16String:
		return text.WTF8()
	default:
		return ""
	}
}
