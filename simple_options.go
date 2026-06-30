// Simple options — maps unified ThinkingLevel to provider-specific options.
package goai

const (
	contextSafetyTokens = 4096
	minMaxTokens        = 1
)

var extendedThinkingLevels = []ModelThinkingLevel{ThinkingOff, ModelThinkingLevel(ThinkingMinimal), ModelThinkingLevel(ThinkingLow), ModelThinkingLevel(ThinkingMedium), ModelThinkingLevel(ThinkingHigh), ModelThinkingLevel(ThinkingXHigh)}

// ClampMaxTokensToContext limits maxTokens to the model's remaining context window.
func ClampMaxTokensToContext(model *Model, ctx *Context, maxTokens int) int {
	if maxTokens < minMaxTokens {
		maxTokens = minMaxTokens
	}
	if model == nil || model.ContextWindow <= 0 {
		return maxTokens
	}
	available := model.ContextWindow - EstimateContextTokens(ctx).Tokens - contextSafetyTokens
	if available < minMaxTokens {
		available = minMaxTokens
	}
	if maxTokens > available {
		return available
	}
	return maxTokens
}

// ClampStreamMaxTokens returns the effective request max token cap for a stream.
func ClampStreamMaxTokens(model *Model, ctx *Context, opts *StreamOptions) int {
	maxTokens := 0
	if model != nil {
		maxTokens = model.MaxTokens
	}
	if opts != nil && opts.MaxTokens != nil {
		maxTokens = *opts.MaxTokens
	}
	return ClampMaxTokensToContext(model, ctx, maxTokens)
}

// ClampStreamMaxTokensPtr returns a stable pointer to the effective request max token cap.
func ClampStreamMaxTokensPtr(model *Model, ctx *Context, opts *StreamOptions) *int {
	v := ClampStreamMaxTokens(model, ctx, opts)
	return &v
}

// ClampReasoning downgrades xhigh to high for legacy callers that do not pass a model.
func ClampReasoning(level ThinkingLevel) ThinkingLevel {
	if level == ThinkingXHigh {
		return ThinkingHigh
	}
	return level
}

// GetSupportedThinkingLevels returns the levels supported by a model, including
// "off" unless the model explicitly disables it with a nil ThinkingLevelMap
// entry. Absent non-xhigh map entries use the provider default spelling and are
// therefore treated as supported; nil entries explicitly disable a level.
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
		if len(available) > 0 {
			return available[0]
		}
		return ModelThinkingLevel(ThinkingOff)
	}
	// Search higher levels first (upstream parity)
	for i := idx; i < len(extendedThinkingLevels); i++ {
		for _, candidate := range available {
			if candidate == extendedThinkingLevels[i] {
				return candidate
			}
		}
	}
	// Then search lower levels
	for i := idx - 1; i >= 0; i-- {
		for _, candidate := range available {
			if candidate == extendedThinkingLevels[i] {
				return candidate
			}
		}
	}
	if len(available) > 0 {
		return available[0]
	}
	return ModelThinkingLevel(ThinkingOff)
}

// MapThinkingLevel returns the provider/model-specific value for a thinking level.
func MapThinkingLevel(model *Model, level ModelThinkingLevel) (string, bool) {
	clamped := ClampThinkingLevel(model, level)
	if model != nil && model.ThinkingLevelMap != nil {
		if mapped, ok := model.ThinkingLevelMap[clamped]; ok {
			if mapped == nil {
				return "", false
			}
			return *mapped, true
		}
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
	if modelMaxTokens > 0 && maxTokens > modelMaxTokens {
		maxTokens = modelMaxTokens
	}
	if maxTokens < 0 {
		maxTokens = 0
	}
	// Mirror upstream: only carve out room for output when thinking would
	// consume the entire max_tokens budget (maxTokens <= thinkingBudget).
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
	if model == nil || usage == nil {
		return CostBreakdown{}
	}
	m := 1_000_000.0
	longWrite := usage.CacheWrite1h
	if longWrite < 0 {
		longWrite = 0
	}
	if longWrite > usage.CacheWrite {
		longWrite = usage.CacheWrite
	}
	shortWrite := usage.CacheWrite - longWrite
	c := CostBreakdown{
		Input:      float64(usage.Input) * model.Cost.Input / m,
		Output:     float64(usage.Output) * model.Cost.Output / m,
		CacheRead:  float64(usage.CacheRead) * model.Cost.CacheRead / m,
		CacheWrite: (float64(shortWrite)*model.Cost.CacheWrite + float64(longWrite)*model.Cost.Input*2) / m,
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

// GetThinkingBudget returns the token budget for a given thinking level,
// using custom budgets if provided, otherwise returning a sensible default.
func GetThinkingBudget(level ThinkingLevel, custom *ThinkingBudgets) int {
	if custom != nil {
		switch level {
		case ThinkingMinimal:
			if custom.Minimal != nil {
				return *custom.Minimal
			}
		case ThinkingLow:
			if custom.Low != nil {
				return *custom.Low
			}
		case ThinkingMedium:
			if custom.Medium != nil {
				return *custom.Medium
			}
		case ThinkingHigh, ThinkingXHigh:
			if custom.High != nil {
				return *custom.High
			}
		}
	}
	// Default budgets
	switch level {
	case ThinkingMinimal:
		return 1024
	case ThinkingLow:
		return 2048
	case ThinkingMedium:
		return 8192
	case ThinkingHigh, ThinkingXHigh:
		return 16384
	default:
		return 8192
	}
}
