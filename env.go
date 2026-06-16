// Full env-var mapping for all known providers.
package goai

import "os"

// providerEnvMap maps provider names to their API key environment variables.
var providerEnvMap = map[Provider][]string{
	ProviderOpenAI:              {"OPENAI_API_KEY"},
	ProviderAnthropic:           {"ANTHROPIC_OAUTH_TOKEN", "ANTHROPIC_API_KEY"},
	ProviderGoogle:              {"GEMINI_API_KEY"},
	ProviderGoogleVertex:        {"GOOGLE_CLOUD_API_KEY"},
	ProviderAzureOpenAI:         {"AZURE_OPENAI_API_KEY"},
	ProviderGitHubCopilot:       {"COPILOT_GITHUB_TOKEN", "GH_TOKEN", "GITHUB_TOKEN"},
	ProviderMistral:             {"MISTRAL_API_KEY"},
	ProviderXAI:                 {"XAI_API_KEY"},
	ProviderGroq:                {"GROQ_API_KEY"},
	ProviderCerebras:            {"CEREBRAS_API_KEY"},
	ProviderOpenRouter:          {"OPENROUTER_API_KEY"},
	ProviderVercelAIGateway:     {"AI_GATEWAY_API_KEY"},
	ProviderZAI:                 {"ZAI_API_KEY"},
	ProviderMiniMax:             {"MINIMAX_API_KEY"},
	ProviderMiniMaxCN:           {"MINIMAX_CN_API_KEY"},
	ProviderHuggingFace:         {"HF_TOKEN"},
	ProviderFireworks:           {"FIREWORKS_API_KEY"},
	ProviderOpenCode:            {"OPENCODE_API_KEY"},
	ProviderOpenCodeGo:          {"OPENCODE_API_KEY"},
	ProviderKimiCoding:          {"KIMI_API_KEY"},
	ProviderDeepSeek:            {"DEEPSEEK_API_KEY"},
	ProviderMoonshotAI:          {"MOONSHOT_API_KEY"},
	ProviderMoonshotAICN:        {"MOONSHOT_API_KEY"},
	ProviderCloudflareAIGateway: {"CLOUDFLARE_API_KEY"},
	ProviderCloudflareWorkersAI: {"CLOUDFLARE_API_KEY"},
	ProviderXiaomi:              {"XIAOMI_API_KEY"},
	ProviderXiaomiTokenPlanCN:   {"XIAOMI_TOKEN_PLAN_CN_API_KEY"},
	ProviderXiaomiTokenPlanAMS:  {"XIAOMI_TOKEN_PLAN_AMS_API_KEY"},
	ProviderXiaomiTokenPlanSGP:  {"XIAOMI_TOKEN_PLAN_SGP_API_KEY"},
	ProviderTogether:            {"TOGETHER_API_KEY"},
	ProviderAntLing:             {"ANT_LING_API_KEY"},
	ProviderNvidia:              {"NVIDIA_API_KEY"},
	ProviderZAICodingCN:         {"ZAI_CODING_CN_API_KEY"},
}

// GetProviderEnvValue returns a provider-scoped environment override when
// present, otherwise it falls back to the process environment.
func ProviderEnvFromOptions(opts *StreamOptions) ProviderEnv {
	if opts == nil {
		return nil
	}
	return opts.Env
}

func GetProviderEnvValue(name string, env ProviderEnv) string {
	if env != nil {
		if v, ok := env[name]; ok {
			return v
		}
	}
	return os.Getenv(name)
}

func ResolveCacheRetention(cacheRetention CacheRetention, env ProviderEnv) CacheRetention {
	if cacheRetention != "" {
		return cacheRetention
	}
	if GetProviderEnvValue("PI_CACHE_RETENTION", env) == "long" {
		return CacheRetentionLong
	}
	return CacheRetentionShort
}

// GetEnvAPIKey looks up an API key from environment variables
// using the same conventions as pi-ai.
func GetEnvAPIKey(provider Provider) string {
	return GetEnvAPIKeyWithEnv(provider, nil)
}

// GetEnvAPIKeyWithEnv looks up an API key, preferring provider-scoped env
// overrides over the process environment.
func GetEnvAPIKeyWithEnv(provider Provider, env ProviderEnv) string {
	envNames, ok := providerEnvMap[provider]
	if ok {
		for _, name := range envNames {
			if v := GetProviderEnvValue(name, env); v != "" {
				return v
			}
		}
		if provider == ProviderGoogleVertex && hasVertexADCCredentials(env) && (GetProviderEnvValue("GOOGLE_CLOUD_PROJECT", env) != "" || GetProviderEnvValue("GCLOUD_PROJECT", env) != "") && GetProviderEnvValue("GOOGLE_CLOUD_LOCATION", env) != "" {
			return "<authenticated>"
		}
		return ""
	}
	if provider == ProviderAmazonBedrock && hasBedrockCredentials(env) {
		return "<authenticated>"
	}
	// Fallback: try PROVIDER_API_KEY pattern
	return envFallback(provider, env)
}

// ResolveAPIKey returns the API key for a request, checking in order:
// 1. Explicit option
// 2. Model-level key
// 3. Provider-scoped/process environment variable (via GetEnvAPIKeyWithEnv)
func ResolveAPIKey(model *Model, opts *StreamOptions) string {
	if opts != nil && opts.APIKey != "" {
		return opts.APIKey
	}
	if model != nil && model.APIKey != "" {
		return model.APIKey
	}
	if model != nil {
		var env ProviderEnv
		if opts != nil {
			env = opts.Env
		}
		return GetEnvAPIKeyWithEnv(model.Provider, env)
	}
	return ""
}

func hasVertexADCCredentials(env ProviderEnv) bool {
	if p := GetProviderEnvValue("GOOGLE_APPLICATION_CREDENTIALS", env); p != "" {
		_, err := os.Stat(p)
		return err == nil
	}
	if home, err := os.UserHomeDir(); err == nil {
		_, err = os.Stat(home + "/.config/gcloud/application_default_credentials.json")
		return err == nil
	}
	return false
}

func hasBedrockCredentials(env ProviderEnv) bool {
	if GetProviderEnvValue("AWS_PROFILE", env) != "" || GetProviderEnvValue("AWS_BEARER_TOKEN_BEDROCK", env) != "" {
		return true
	}
	if GetProviderEnvValue("AWS_ACCESS_KEY_ID", env) != "" && GetProviderEnvValue("AWS_SECRET_ACCESS_KEY", env) != "" {
		return true
	}
	if GetProviderEnvValue("AWS_CONTAINER_CREDENTIALS_RELATIVE_URI", env) != "" || GetProviderEnvValue("AWS_CONTAINER_CREDENTIALS_FULL_URI", env) != "" {
		return true
	}
	return GetProviderEnvValue("AWS_WEB_IDENTITY_TOKEN_FILE", env) != ""
}

func envFallback(provider Provider, env ProviderEnv) string {
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
	return GetProviderEnvValue(upper+"_API_KEY", env)
}
