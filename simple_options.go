// Simple options — maps unified ThinkingLevel to provider-specific options.
package goai

var extendedThinkingLevels = []ModelThinkingLevel{ThinkingOff, ModelThinkingLevel(ThinkingMinimal), ModelThinkingLevel(ThinkingLow), ModelThinkingLevel(ThinkingMedium), ModelThinkingLevel(ThinkingHigh), ModelThinkingLevel(ThinkingXHigh)}

// ClampReasoning downgrades xhigh to high for legacy callers that do not pass a model.
func ClampReasoning(level ThinkingLevel) ThinkingLevel {
	if level == ThinkingXHigh {
		return ThinkingHigh
	}
	return level
}

// GetSupportedThinkingLevels returns the levels supported by a model, including "off".
func GetSupportedThinkingLevels(model *Model) []ModelThinkingLevel {
	if model == nil || !model.Reasoning {
		return []ModelThinkingLevel{ThinkingOff}
	}
	out := make([]ModelThinkingLevel, 0, len(extendedThinkingLevels))
	for _, level := range extendedThinkingLevels {
		mapped, ok := model.ThinkingLevelMap[level]
		if ok && mapped == nil {
			continue
		}
		if level == ModelThinkingLevel(ThinkingXHigh) && !ok {
			continue
		}
		out = append(out, level)
	}
	if len(out) == 0 {
		return []ModelThinkingLevel{ThinkingOff}
	}
	return out
}

// ClampThinkingLevel clamps a requested level to the nearest supported model level.
func ClampThinkingLevel(model *Model, level ModelThinkingLevel) ModelThinkingLevel {
	available := GetSupportedThinkingLevels(model)
	for _, candidate := range available {
		if candidate == level {
			return level
		}
	}
	idx := -1
	for i, candidate := range extendedThinkingLevels {
		if candidate == level {
			idx = i
			break
		}
	}
	if idx < 0 {
		return available[0]
	}
	for i := idx; i < len(extendedThinkingLevels); i++ {
		for _, candidate := range available {
			if candidate == extendedThinkingLevels[i] {
				return candidate
			}
		}
	}
	for i := idx - 1; i >= 0; i-- {
		for _, candidate := range available {
			if candidate == extendedThinkingLevels[i] {
				return candidate
			}
		}
	}
	return available[0]
}

// MapThinkingLevel returns the provider/model-specific value for a thinking level.
func MapThinkingLevel(model *Model, level ModelThinkingLevel) (string, bool) {
	clamped := ClampThinkingLevel(model, level)
	if mapped, ok := model.ThinkingLevelMap[clamped]; ok {
		if mapped == nil {
			return "", false
		}
		return *mapped, true
	}
	if clamped == ThinkingOff {
		return "none", true
	}
	return string(clamped), true
}

// DefaultThinkingBudgets returns the default token budgets per thinking level.
func DefaultThinkingBudgets() ThinkingBudgets {
	return ThinkingBudgets{
		intPtr(1024),  // minimal
		intPtr(2048),  // low
		intPtr(8192),  // medium
		intPtr(16384), // high
	}
}

// AdjustMaxTokensForThinking computes the maxTokens and thinkingBudget
// for a given reasoning level, ensuring the total fits in the model's limit.
func AdjustMaxTokensForThinking(baseMaxTokens, modelMaxTokens int, level ThinkingLevel, custom *ThinkingBudgets) (maxTokens, thinkingBudget int) {
	defaults := DefaultThinkingBudgets()
	budgets := mergeThinkingBudgets(defaults, custom)

	clamped := ClampReasoning(level)
	thinkingBudget = budgetForLevel(budgets, clamped)

	const minOutputTokens = 1024
	maxTokens = baseMaxTokens + thinkingBudget
	if maxTokens > modelMaxTokens {
		maxTokens = modelMaxTokens
	}
	if maxTokens <= thinkingBudget {
		thinkingBudget = maxTokens - minOutputTokens
		if thinkingBudget < 0 {
			thinkingBudget = 0
		}
	}

	return maxTokens, thinkingBudget
}

// CalculateCost computes the cost breakdown from usage and model pricing.
func CalculateCost(model *Model, usage *Usage) CostBreakdown {
	m := 1_000_000.0
	c := CostBreakdown{
		Input:      float64(usage.Input) * model.Cost.Input / m,
		Output:     float64(usage.Output) * model.Cost.Output / m,
		CacheRead:  float64(usage.CacheRead) * model.Cost.CacheRead / m,
		CacheWrite: float64(usage.CacheWrite) * model.Cost.CacheWrite / m,
	}
	c.Total = c.Input + c.Output + c.CacheRead + c.CacheWrite
	return c
}

// SupportsXhigh checks if a model supports the xhigh thinking level.
func SupportsXhigh(model *Model) bool {
	for _, level := range GetSupportedThinkingLevels(model) {
		if level == ModelThinkingLevel(ThinkingXHigh) {
			return true
		}
	}
	return false
}

// ModelsAreEqual compares two models by ID and provider.
func ModelsAreEqual(a, b *Model) bool {
	if a == nil || b == nil {
		return false
	}
	return a.ID == b.ID && a.Provider == b.Provider
}

func intPtr(v int) *int { return &v }

func mergeThinkingBudgets(base ThinkingBudgets, custom *ThinkingBudgets) ThinkingBudgets {
	if custom == nil {
		return base
	}
	if custom.Minimal != nil {
		base.Minimal = custom.Minimal
	}
	if custom.Low != nil {
		base.Low = custom.Low
	}
	if custom.Medium != nil {
		base.Medium = custom.Medium
	}
	if custom.High != nil {
		base.High = custom.High
	}
	return base
}

func budgetForLevel(b ThinkingBudgets, level ThinkingLevel) int {
	switch level {
	case ThinkingMinimal:
		if b.Minimal != nil {
			return *b.Minimal
		}
		return 1024
	case ThinkingLow:
		if b.Low != nil {
			return *b.Low
		}
		return 2048
	case ThinkingMedium:
		if b.Medium != nil {
			return *b.Medium
		}
		return 8192
	case ThinkingHigh:
		if b.High != nil {
			return *b.High
		}
		return 16384
	default:
		return 8192
	}
}
