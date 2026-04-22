// Overflow detection — identifies context window exceeded errors across providers.
package goai

import "regexp"

// Regex patterns to detect context overflow errors from different providers.
var overflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)prompt is too long`),                           // Anthropic token overflow
	regexp.MustCompile(`(?i)request_too_large`),                            // Anthropic request byte-size overflow (HTTP 413)
	regexp.MustCompile(`(?i)input is too long for requested model`),        // Amazon Bedrock
	regexp.MustCompile(`(?i)exceeds the context window`),                   // OpenAI (Completions & Responses)
	regexp.MustCompile(`(?i)input token count.*exceeds the maximum`),       // Google (Gemini)
	regexp.MustCompile(`(?i)maximum prompt length is \d+`),                 // xAI (Grok)
	regexp.MustCompile(`(?i)reduce the length of the messages`),            // Groq
	regexp.MustCompile(`(?i)maximum context length is \d+ tokens`),         // OpenRouter
	regexp.MustCompile(`(?i)exceeds the limit of \d+`),                     // GitHub Copilot
	regexp.MustCompile(`(?i)exceeds the available context size`),           // llama.cpp
	regexp.MustCompile(`(?i)greater than the context length`),              // LM Studio
	regexp.MustCompile(`(?i)context window exceeds limit`),                 // MiniMax
	regexp.MustCompile(`(?i)exceeded model token limit`),                   // Kimi For Coding
	regexp.MustCompile(`(?i)too large for model with \d+ maximum context length`), // Mistral
	regexp.MustCompile(`(?i)model_context_window_exceeded`),                // z.ai
	regexp.MustCompile(`(?i)prompt too long; exceeded (?:max )?context length`), // Ollama
	regexp.MustCompile(`(?i)context[_ ]length[_ ]exceeded`),               // Generic
	regexp.MustCompile(`(?i)too many tokens`),                              // Generic
	regexp.MustCompile(`(?i)token limit exceeded`),                         // Generic
	regexp.MustCompile(`(?i)^4(?:00|13)\s*(?:status code)?\s*\(no body\)`), // Cerebras
}

// Non-overflow patterns — errors that match overflow patterns but aren't overflow.
var nonOverflowPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(Throttling error|Service unavailable):`), // AWS Bedrock
	regexp.MustCompile(`(?i)rate limit`),
	regexp.MustCompile(`(?i)too many requests`),
}

// IsContextOverflow checks if a message represents a context window overflow.
//
// Handles two cases:
//  1. Error-based: stopReason="error" with a provider-specific error message.
//  2. Silent overflow: successful response but usage.input > contextWindow (z.ai).
//
// Pass contextWindow=0 to skip silent overflow detection.
func IsContextOverflow(msg *Message, contextWindow int) bool {
	// Case 1: error message patterns
	if msg.StopReason == StopReasonError && msg.ErrorMessage != "" {
		// Exclude non-overflow errors (throttling, rate limits)
		for _, p := range nonOverflowPatterns {
			if p.MatchString(msg.ErrorMessage) {
				return false
			}
		}
		for _, p := range overflowPatterns {
			if p.MatchString(msg.ErrorMessage) {
				return true
			}
		}
	}

	// Case 2: silent overflow (z.ai style)
	if contextWindow > 0 && msg.StopReason == StopReasonStop && msg.Usage != nil {
		inputTokens := msg.Usage.Input + msg.Usage.CacheRead
		if inputTokens > contextWindow {
			return true
		}
	}

	return false
}
