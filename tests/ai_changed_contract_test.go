package goai_test

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
)

type changedAIUnicodeFixture struct {
	Emoji    string `json:"emoji"`
	LinkedIn string `json:"linkedin"`
	Unpaired struct {
		Prefix    string `json:"prefix"`
		Suffix    string `json:"suffix"`
		UTF16High uint16 `json:"utf16_high"`
	} `json:"unpaired"`
}

func loadChangedAIUnicodeFixture(t *testing.T) changedAIUnicodeFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/ai-contract/unicode-tool-results.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture changedAIUnicodeFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func changedAICompletion(t *testing.T, messages []goai.Message, mutate *func(*goai.StreamOptions)) ([]byte, int, *goai.Message) {
	t.Helper()
	model := &goai.Model{Api: goai.ApiOpenAICompletions, Provider: "test-provider", ID: "test-model", BaseURL: "https://test.invalid/v1", ContextWindow: 8192, MaxTokens: 256, Input: []string{"text"}}
	var wire atomic.Value
	var requests atomic.Int32
	client := &http.Client{Transport: &fetchOptionWire{roundTrip: func(req *http.Request) (*http.Response, error) {
		requests.Add(1)
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		wire.Store(raw)
		return &http.Response{
			Request: req, StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"text/event-stream"}},
			Body:   io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"4\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"prompt_tokens_details\":{\"cached_tokens\":3,\"cache_write_tokens\":2},\"completion_tokens\":1,\"total_tokens\":11}}\n\ndata: [DONE]\n\n")),
		}, nil
	}}}
	options := &goai.StreamOptions{APIKey: "secret", HTTPTransport: client.Transport}
	if mutate != nil {
		(*mutate)(options)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	var done *goai.Message
	var terminalErr error
	for event := range goai.Stream(ctx, model, &goai.Context{Messages: messages}, options) {
		switch event := event.(type) {
		case *goai.DoneEvent:
			done = event.Message
		case *goai.ErrorEvent:
			terminalErr = event.Err
		}
	}
	if terminalErr != nil && requests.Load() != 0 {
		t.Fatalf("terminal error after request: %v", terminalErr)
	}
	if terminalErr == nil && (done == nil || done.StopReason != goai.StopReasonStop) {
		t.Fatalf("terminal message=%+v", done)
	}
	if raw, ok := wire.Load().([]byte); ok {
		return raw, int(requests.Load()), done
	}
	return nil, int(requests.Load()), done
}

func changedAIHistory(callID, toolName string, result *string, followup string, api goai.Api, provider goai.Provider, model string) []goai.Message {
	messages := []goai.Message{
		goai.UserMessage("Use the tool"),
		{Role: goai.RoleAssistant, Api: api, Provider: provider, Model: model, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: callID, Name: toolName, Arguments: map[string]any{}}}},
	}
	if result != nil {
		messages = append(messages, goai.Message{Role: goai.RoleToolResult, ToolCallID: callID, ToolName: toolName, Content: []goai.ContentBlock{{Type: "text", Text: *result}}})
	}
	return append(messages, goai.UserMessage(followup))
}

// Source: pi v1.7.0 packages/ai/test/tool-call-without-result.test.ts:32–90.
func TestAIChangedToolCallWithoutResultUsesProductionTranscript(t *testing.T) {
	messages := changedAIHistory("call-1", "calculate", nil, "Never mind, just tell me what is 2+2?", goai.ApiOpenAICompletions, "test-provider", "test-model")
	wire, requests, _ := changedAICompletion(t, messages, nil)
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
	var body struct {
		Messages []struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			Content    any    `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(wire, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Messages) != 4 || body.Messages[0].Role != "user" || body.Messages[1].Role != "assistant" || body.Messages[2].Role != "tool" || body.Messages[3].Role != "user" {
		t.Fatalf("wire roles=%+v", body.Messages)
	}
	if body.Messages[2].ToolCallID != "call-1" || body.Messages[2].Content != "No result provided" {
		t.Fatalf("synthetic result=%+v", body.Messages[2])
	}
}

// Source: pi v1.7.0 packages/ai/test/total-tokens.test.ts:47–98,155–177.
func TestAIChangedTotalTokensUsesProductionUsage(t *testing.T) {
	_, requests, done := changedAICompletion(t, []goai.Message{goai.UserMessage("What is 2 + 2?")}, nil)
	if requests != 1 || done == nil || done.Usage == nil {
		t.Fatalf("requests=%d message=%+v", requests, done)
	}
	usage := done.Usage
	if usage.Input != 5 || usage.Output != 1 || usage.CacheRead != 3 || usage.CacheWrite != 2 || usage.TotalTokens != 11 {
		t.Fatalf("usage=%+v", usage)
	}
	computed := usage.Input + usage.Output + usage.CacheRead + usage.CacheWrite
	if usage.TotalTokens != computed {
		t.Fatalf("totalTokens=%d, computed=%d", usage.TotalTokens, computed)
	}
}

// Source: pi v1.7.0 packages/ai/test/unicode-surrogate.test.ts:35–119,122–208,211–282.
func TestAIChangedUnicodeUsesExactProductionToolResultText(t *testing.T) {
	fixture := loadChangedAIUnicodeFixture(t)
	if fixture.Unpaired.UTF16High != 0xd83d {
		t.Fatalf("source unpaired high surrogate=%#x, want 0xd83d", fixture.Unpaired.UTF16High)
	}
	// U+D83D is a valid JavaScript code unit but not a valid UTF-8 scalar.
	// WTF-8 preserves that exact unit for the production sanitizer boundary.
	unpaired := fixture.Unpaired.Prefix + string([]byte{0xed, 0xa0, 0xbd}) + fixture.Unpaired.Suffix
	for _, tc := range []struct{ name, text string }{
		{"emoji", fixture.Emoji},
		{"linkedin", fixture.LinkedIn},
		{"unpaired-high-surrogate", unpaired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			messages := changedAIHistory("test_1", "test_tool", &tc.text, "Summarize the tool result briefly.", goai.ApiOpenAICompletions, "test-provider", "test-model")
			wire, requests, _ := changedAICompletion(t, messages, nil)
			if requests != 1 || !json.Valid(wire) {
				t.Fatalf("requests=%d valid=%v", requests, json.Valid(wire))
			}
			if strings.Contains(string(wire), `\ufffd`) || strings.Contains(string(wire), `\ud83d`) || strings.Contains(string(wire), string([]byte{0xed, 0xa0, 0xbd})) {
				t.Fatalf("wire retained an invalid surrogate: %s", wire)
			}
			if tc.name != "unpaired-high-surrogate" && !strings.Contains(string(wire), "🙈") {
				t.Fatalf("wire lost exact Unicode source text: %s", wire)
			}
		})
	}
}

// A mutation hook can no longer relax Go's strict finite JSON contract.
func TestAIChangedStrictFiniteJSONControl(t *testing.T) {
	mutate := func(options *goai.StreamOptions) {
		options.OnPayload = func(any, *goai.Model) (any, error) { return map[string]any{"invalid": math.Inf(1)}, nil }
	}
	wire, requests, done := changedAICompletion(t, []goai.Message{goai.UserMessage("hi")}, &mutate)
	if done != nil {
		t.Fatalf("non-finite payload produced a terminal message: %+v", done)
	}
	if requests != 0 || wire != nil {
		t.Fatalf("non-finite payload sent: requests=%d wire=%s", requests, wire)
	}
}
