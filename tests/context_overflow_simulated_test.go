package goai_test

import (
	"regexp"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func simulatedOverflowMessage(provider goai.Provider, model string, stop goai.StopReason, errorMessage string, usage *goai.Usage) *goai.Message {
	if usage == nil {
		usage = &goai.Usage{}
	}
	return &goai.Message{Role: goai.RoleAssistant, Provider: provider, Model: model, StopReason: stop, ErrorMessage: errorMessage, Usage: usage}
}

func assertSimulatedOverflow(t *testing.T, name string, msg *goai.Message, contextWindow int, pattern string) {
	t.Helper()
	if msg.StopReason == goai.StopReasonError && pattern != "" && !regexp.MustCompile(pattern).MatchString(msg.ErrorMessage) {
		t.Fatalf("%s errorMessage=%q does not match %s", name, msg.ErrorMessage, pattern)
	}
	if !goai.IsContextOverflow(msg, contextWindow) {
		t.Fatalf("%s should be detected as context overflow: %#v", name, msg)
	}
}

func TestContextOverflowSimulatedAnthropicAPIKey(t *testing.T) {
	assertSimulatedOverflow(t, "anthropic api key", simulatedOverflowMessage(goai.ProviderAnthropic, "claude-haiku-4-5", goai.StopReasonError, "prompt is too long: 213462 tokens > 200000 maximum", nil), 200000, `(?i)prompt is too long`)
}

func TestContextOverflowSimulatedGitHubCopilotGemini(t *testing.T) {
	assertSimulatedOverflow(t, "copilot gemini", simulatedOverflowMessage(goai.ProviderGitHubCopilot, "gemini-2.5-pro", goai.StopReasonError, "input exceeds the limit of 1048576 tokens", nil), 1048576, `(?i)exceeds the limit of \d+`)
}

func TestContextOverflowSimulatedOpenAICompletions(t *testing.T) {
	assertSimulatedOverflow(t, "openai completions", simulatedOverflowMessage(goai.ProviderOpenAI, "gpt-4o-mini", goai.StopReasonError, "Requested token count exceeds the model's maximum context length of 128000 tokens.", nil), 128000, `(?i)maximum context length`)
}

func TestContextOverflowSimulatedOpenAIResponses(t *testing.T) {
	assertSimulatedOverflow(t, "openai responses", simulatedOverflowMessage(goai.ProviderOpenAI, "gpt-4o", goai.StopReasonError, "This request exceeds the context window for the model.", nil), 128000, `(?i)exceeds the context window`)
}

func TestContextOverflowSimulatedGoogle(t *testing.T) {
	assertSimulatedOverflow(t, "google", simulatedOverflowMessage(goai.ProviderGoogle, "gemini-2.0-flash", goai.StopReasonError, "input token count 1049000 exceeds the maximum of 1048576", nil), 1048576, `(?i)input token count.*exceeds the maximum`)
}

func TestContextOverflowSimulatedXAI(t *testing.T) {
	assertSimulatedOverflow(t, "xai", simulatedOverflowMessage(goai.ProviderXAI, "grok-3-fast", goai.StopReasonError, "maximum prompt length is 131072 tokens", nil), 131072, `(?i)maximum prompt length is \d+`)
}

func TestContextOverflowSimulatedGroq(t *testing.T) {
	assertSimulatedOverflow(t, "groq", simulatedOverflowMessage(goai.ProviderGroq, "llama-3.3-70b-versatile", goai.StopReasonError, "Please reduce the length of the messages", nil), 131072, `(?i)reduce the length of the messages`)
}

func TestContextOverflowSimulatedCerebras(t *testing.T) {
	assertSimulatedOverflow(t, "cerebras", simulatedOverflowMessage(goai.ProviderCerebras, "gpt-oss-120b", goai.StopReasonError, "HTTP 413: (no body)", nil), 131072, `(?i)4(00|13|29).*\(no body\)`)
}

func TestContextOverflowSimulatedMistral(t *testing.T) {
	assertSimulatedOverflow(t, "mistral", simulatedOverflowMessage(goai.ProviderMistral, "devstral-medium-latest", goai.StopReasonError, "input is too large for model with 128000 maximum context length", nil), 128000, `(?i)too large for model with \d+ maximum context length`)
}

func TestContextOverflowSimulatedOpenRouterBackends(t *testing.T) {
	for _, model := range []string{"anthropic/claude-sonnet-4", "deepseek/deepseek-v3.2", "mistralai/mistral-large-2512", "google/gemini-2.5-flash", "meta-llama/llama-4-scout"} {
		assertSimulatedOverflow(t, "openrouter "+model, simulatedOverflowMessage(goai.ProviderOpenRouter, model, goai.StopReasonError, "maximum context length is 131072 tokens", nil), 131072, `(?i)maximum context length is \d+ tokens`)
	}
}

func TestContextOverflowSimulatedZAIExplicitOverflow(t *testing.T) {
	assertSimulatedOverflow(t, "zai explicit", simulatedOverflowMessage(goai.ProviderZAI, "glm-4.5-air", goai.StopReasonError, "model_context_window_exceeded", nil), 131072, `(?i)model_context_window_exceeded`)
}

func TestContextOverflowSimulatedZAISilentOverflowUsage(t *testing.T) {
	assertSimulatedOverflow(t, "zai silent", simulatedOverflowMessage(goai.ProviderZAI, "glm-4.5-air", goai.StopReasonStop, "", &goai.Usage{Input: 140000}), 131072, "")
}

func TestContextOverflowSimulatedXiaomiLengthStopOverflow(t *testing.T) {
	assertSimulatedOverflow(t, "xiaomi length stop", simulatedOverflowMessage(goai.ProviderXiaomi, "mimo-v2.5-pro", goai.StopReasonLength, "", &goai.Usage{Input: 58, CacheRead: 1048512, Output: 0}), 1048576, "")
}
