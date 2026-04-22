package goai

import (
	"fmt"
	"os"
	"strings"
)

// GetEnvAPIKey looks up an API key from environment variables
// using the same conventions as pi-ai.
func GetEnvAPIKey(provider Provider) string {
	envNames := envKeyNames(provider)
	for _, name := range envNames {
		if v := os.Getenv(name); v != "" {
			return v
		}
	}
	return ""
}

// envKeyNames returns the environment variable names to check for a provider,
// matching pi-ai's convention.
func envKeyNames(provider Provider) []string {
	p := string(provider)
	upper := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(p, "-", "_"), ".", "_"))
	return []string{
		fmt.Sprintf("%s_API_KEY", upper),
		fmt.Sprintf("%sAPI_KEY", upper), // e.g., OPENAI_API_KEY
	}
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
