// Utility functions — hashing, sanitization, and provider-specific helpers.
package goai

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
)

// --- Hashing ---

// ShortHash returns a short deterministic hash of a string.
// Used for normalizing long IDs (e.g., OpenAI Responses tool call IDs).
func ShortHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// --- Unicode sanitization ---

// SanitizeSurrogates removes unpaired Unicode surrogate characters from a string.
// Valid emoji and other properly paired surrogates are preserved.
func SanitizeSurrogates(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		if r == 0xFFFD || (r >= 0xD800 && r <= 0xDFFF) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// --- GitHub Copilot headers ---

// CopilotHeaders returns the standard headers required for GitHub Copilot API calls.
func CopilotHeaders() map[string]string {
	return map[string]string{
		"User-Agent":             "GitHubCopilotChat/0.35.0",
		"Editor-Version":        "vscode/1.107.0",
		"Editor-Plugin-Version": "copilot-chat/0.35.0",
		"Copilot-Integration-Id": "vscode-chat",
	}
}

// CopilotHeadersWithIntent returns Copilot headers plus an intent header.
func CopilotHeadersWithIntent(intent string) map[string]string {
	h := CopilotHeaders()
	if intent != "" {
		h["openai-intent"] = intent
	}
	return h
}

// --- Cloudflare Workers AI ---

// ResolveCloudflareBaseURL substitutes {VAR} placeholders in a Cloudflare
// base URL from environment variables (e.g., {CLOUDFLARE_ACCOUNT_ID}).
func ResolveCloudflareBaseURL(model *Model) string {
	url := model.BaseURL
	if !strings.Contains(url, "{") {
		return url
	}
	result := url
	for {
		start := strings.Index(result, "{")
		if start < 0 {
			break
		}
		end := strings.Index(result[start:], "}")
		if end < 0 {
			break
		}
		name := result[start+1 : start+end]
		value := os.Getenv(name)
		if value == "" {
			logWarn("cloudflare base URL placeholder not set", "var", name, "provider", model.Provider)
		}
		result = result[:start] + value + result[start+end+1:]
	}
	return result
}

// IsCloudflareProvider returns true if the provider is Cloudflare Workers AI.
func IsCloudflareProvider(provider Provider) bool {
	return provider == ProviderCloudflareWorkersAI || provider == ProviderCloudflareAIGateway
}
