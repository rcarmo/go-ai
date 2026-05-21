package goai

const OpenAIPromptCacheKeyMaxLength = 64

// ClampOpenAIPromptCacheKey trims OpenAI prompt cache keys to the upstream
// pi-ai limit of 64 Unicode code points.
func ClampOpenAIPromptCacheKey(key string) string {
	if key == "" {
		return ""
	}
	runes := []rune(key)
	if len(runes) <= OpenAIPromptCacheKeyMaxLength {
		return key
	}
	return string(runes[:OpenAIPromptCacheKeyMaxLength])
}
