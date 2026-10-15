package mistral

import "encoding/json"

// Normalise only SDK request fields. Arbitrary schema/tool argument keys are
// caller data, so recursive snake-case conversion would corrupt their meaning.
func marshalMistralPayload(payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	rename := func(object map[string]json.RawMessage, from, to string) {
		if value, ok := object[from]; ok {
			if _, explicit := object[to]; !explicit {
				object[to] = value
			}
			delete(object, from)
		}
	}
	for from, to := range map[string]string{"maxTokens": "max_tokens", "promptMode": "prompt_mode", "reasoningEffort": "reasoning_effort", "promptCacheKey": "prompt_cache_key", "topP": "top_p", "randomSeed": "random_seed", "presencePenalty": "presence_penalty", "frequencyPenalty": "frequency_penalty", "parallelToolCalls": "parallel_tool_calls", "safePrompt": "safe_prompt", "toolChoice": "tool_choice", "responseFormat": "response_format"} {
		rename(body, from, to)
	}
	if value, ok := body["response_format"]; ok {
		var format map[string]json.RawMessage
		if err := json.Unmarshal(value, &format); err == nil && format != nil {
			rename(format, "jsonSchema", "json_schema")
			if value, ok := format["json_schema"]; ok {
				var schema map[string]json.RawMessage
				if err := json.Unmarshal(value, &schema); err == nil && schema != nil {
					rename(schema, "schemaDefinition", "schema")
					format["json_schema"], err = json.Marshal(schema)
					if err != nil {
						return nil, err
					}
				}
			}
			body["response_format"], err = json.Marshal(format)
			if err != nil {
				return nil, err
			}
		}
	}
	return json.Marshal(body)
}
