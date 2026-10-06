package goai

// ResolveSamplingParams follows the official model -> clamped thinking level ->
// request precedence. The returned map is owned; neither input map is changed.
func ResolveSamplingParams(model *Model, level ModelThinkingLevel, overrides map[string]any) map[string]any {
	var base, thinking map[string]any
	if model != nil {
		base = model.SamplingParams
		resolved := ClampThinkingLevel(model, level)
		thinking = model.SamplingParamsByThinkingLevel[resolved]
	}
	if len(base)+len(thinking)+len(overrides) == 0 {
		return nil
	}
	result := make(map[string]any, len(base)+len(thinking)+len(overrides))
	for key, value := range base {
		result[key] = value
	}
	for key, value := range thinking {
		result[key] = value
	}
	for key, value := range overrides {
		result[key] = value
	}
	return result
}

func cloneSamplingParams(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	out := make(map[string]any, len(params))
	for key, value := range params {
		out[key] = cloneSamplingValue(value)
	}
	return out
}
func cloneSamplingValue(value any) any {
	switch value := value.(type) {
	case map[string]any:
		return cloneSamplingParams(value)
	case []any:
		copy := make([]any, len(value))
		for i, item := range value {
			copy[i] = cloneSamplingValue(item)
		}
		return copy
	default:
		return value
	}
}
