// Full env-var mapping for all known providers.
package goai

import "os"

// providerEnvMap maps provider names to their API key environment variables.
var providerEnvMap = map[Provider][]string{
	ProviderOpenAI:           {"OPENAI_API_KEY"},
	ProviderAnthropic:        {"ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_API_KEY"},
	ProviderGoogle:           {"GEMINI_API_KEY"},
	ProviderGoogleVertex:     {"GOOGLE_CLOUD_API_KEY"},
	ProviderAzureOpenAI:      {"AZURE_OPENAI_API_KEY"},
	ProviderGitHubCopilot:    {"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"},
	ProviderMistral:          {"MISTRAL_API_KEY"},
	ProviderXAI:              {"XAI_API_KEY"},
	ProviderGroq:             {"GROQ_API_KEY"},
	ProviderCerebras:         {"CEREBRAS_API_KEY"},
	ProviderOpenRouter:       {"OPENROUTER_API_KEY"},
	ProviderVercelAIGateway:  {"AI_GATEWAY_API_KEY"},
	ProviderZAI:              {"ZAI_API_KEY"},
	ProviderMiniMax:          {"MINIMAX_API_KEY"},
	ProviderMiniMaxCN:        {"MINIMAX_CN_API_KEY"},
	ProviderHuggingFace:      {"HF_TOKEN"},
	ProviderFireworks:        {"FIREWORKS_API_KEY"},
	ProviderOpenCode:         {"OPENCODE_API_KEY"},
	ProviderOpenCodeGo:       {"OPENCODE_API_KEY"},
	ProviderKimiCoding:       {"KIMI_API_KEY"},
	ProviderDeepSeek:         {"DEEPSEEK_API_KEY"},
}

// GetEnvAPIKey looks up an API key from environment variables
// using the same conventions as pi-ai.
func GetEnvAPIKey(provider Provider) string {
	envNames, ok := providerEnvMap[provider]
	if ok {
		for _, name := range envNames {
			if v := os.Getenv(name); v != "" {
				return v
			}
		}
		return ""
	}
	// Fallback: try PROVIDER_API_KEY pattern
	return envFallback(provider)
}

// ResolveAPIKey returns the API key for a request, checking in order:
// 1. Explicit option
// 2. Model-level key
// 3. Environment variable
func ResolveAPIKey(model *Model, opts *StreamOptions) string {
	if opts != nil && opts.APIKey != "" {
		return opts.APIKey
	}
	if model.APIKey != "" {
		return model.APIKey
	}
	return GetEnvAPIKey(model.Provider)
}

func envFallback(provider Provider) string {
	// Generic: uppercase, replace - with _, append _API_KEY
	upper := ""
	for _, c := range string(provider) {
		if c == '-' || c == '.' {
			upper += "_"
		} else if c >= 'a' && c <= 'z' {
			upper += string(c - 32)
		} else {
			upper += string(c)
		}
	}
	return os.Getenv(upper + "_API_KEY")
}
