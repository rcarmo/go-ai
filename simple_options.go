// Simple options — maps unified ThinkingLevel to provider-specific options.
package goai

// ClampReasoning downgrades xhigh to high for providers that don't support it.
func ClampReasoning(level ThinkingLevel) ThinkingLevel {
	if level == ThinkingXHigh {
		return ThinkingHigh
	}
	return level
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
	// GPT-5.2+ and Opus 4.6+ families
	id := model.ID
	for _, prefix := range []string{"gpt-5.2", "gpt-5.3", "gpt-5.4", "gpt-5.5", "claude-opus-4.6", "claude-opus-4.7", "deepseek-v4-pro", "deepseek-v4-flash"} {
		if len(id) >= len(prefix) && id[:len(prefix)] == prefix {
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
