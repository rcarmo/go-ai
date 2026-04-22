package openairesponses

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAzureResponsesRequestAppliesCleanupAndSessionHeaders(t *testing.T) {
	var rawReq responsesRequest
	var input []map[string]interface{}
	var gotSessionID, gotClientReqID, gotMSClientReqID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		gotSessionID = r.Header.Get("session_id")
		gotClientReqID = r.Header.Get("x-client-request-id")
		gotMSClientReqID = r.Header.Get("x-ms-client-request-id")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if err := json.Unmarshal(body, &rawReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if err := json.Unmarshal(rawReq.Input, &input); err != nil {
			t.Fatalf("decode input: %v", err)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{
		ID:       "gpt-4.1",
		Provider: goai.ProviderAzureOpenAI,
		Api:      goai.ApiAzureOpenAIResponses,
		BaseURL:  server.URL,
		Reasoning: true,
	}

	var messages []goai.Message
	messages = append(messages, goai.UserMessage("start"))
	for i := 0; i < 130; i++ {
		messages = append(messages,
			goai.Message{
				Role: goai.RoleAssistant,
				Content: []goai.ContentBlock{{
					Type:      "toolCall",
					ID:        "call_" + strings.Repeat("x", 1) + string(rune('a'+(i%26))) + "|item_1",
					Name:      "search",
					Arguments: map[string]interface{}{"q": "query"},
				}},
				StopReason: goai.StopReasonToolUse,
				Provider:   goai.ProviderAzureOpenAI,
				Api:        goai.ApiAzureOpenAIResponses,
				Model:      "gpt-4.1",
			},
			goai.Message{
				Role:       goai.RoleToolResult,
				ToolCallID: "call_" + strings.Repeat("x", 1) + string(rune('a'+(i%26))) + "|item_1",
				ToolName:   "search",
				Content:    []goai.ContentBlock{{Type: "text", Text: "tool output"}},
			},
		)
	}

	convCtx := &goai.Context{Messages: messages}
	opts := &goai.StreamOptions{APIKey: "test-key", SessionID: "sess-123"}

	for range streamResponses(context.Background(), model, convCtx, opts) {
	}

	if gotSessionID != "sess-123" || gotClientReqID != "sess-123" || gotMSClientReqID != "sess-123" {
		t.Fatalf("missing Azure session headers: session_id=%q x-client-request-id=%q x-ms-client-request-id=%q", gotSessionID, gotClientReqID, gotMSClientReqID)
	}

	functionCalls := 0
	sawSummary := false
	for _, item := range input {
		if typ, _ := item["type"].(string); typ == "function_call" {
			functionCalls++
		}
		if typ, _ := item["type"].(string); typ == "message" {
			if content, ok := item["content"].([]interface{}); ok && len(content) > 0 {
				if part, ok := content[0].(map[string]interface{}); ok {
					if text, _ := part["text"].(string); strings.Contains(text, "Earlier tool calls (summarized):") {
						sawSummary = true
					}
				}
			}
		}
	}

	if functionCalls != 128 {
		t.Fatalf("expected 128 kept function_call items after Azure cleanup, got %d", functionCalls)
	}
	if !sawSummary {
		t.Fatal("expected Azure cleanup summary message in request input")
	}
}

func TestAzureResponsesNormalizesCommentaryIntoThinkingEvents(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"item_1\",\"type\":\"message\",\"phase\":\"commentary\"}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.reasoning_text.delta\",\"delta\":\"thinking...\"}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.output_item.done\",\"item\":{\"id\":\"item_1\",\"type\":\"message\",\"phase\":\"commentary\",\"content\":[{\"type\":\"output_text\",\"text\":\"thinking...\"}]}}\n\n"))
		_, _ = w.Write([]byte("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"usage\":{\"input_tokens\":1,\"output_tokens\":1,\"total_tokens\":2}}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{
		ID:        "gpt-4.1",
		Provider:  goai.ProviderAzureOpenAI,
		Api:       goai.ApiAzureOpenAIResponses,
		BaseURL:   server.URL,
		Reasoning: true,
	}
	convCtx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}
	opts := &goai.StreamOptions{APIKey: "test-key"}

	var sawThinkingStart, sawThinkingEnd bool
	var thinking string
	var done *goai.DoneEvent

	for ev := range streamResponses(context.Background(), model, convCtx, opts) {
		switch e := ev.(type) {
		case *goai.ThinkingStartEvent:
			sawThinkingStart = true
		case *goai.ThinkingDeltaEvent:
			thinking += e.Delta
		case *goai.ThinkingEndEvent:
			sawThinkingEnd = true
		case *goai.DoneEvent:
			done = e
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}

	if !sawThinkingStart {
		t.Fatal("expected ThinkingStartEvent after Azure commentary normalization")
	}
	if thinking != "thinking..." {
		t.Fatalf("unexpected thinking delta: %q", thinking)
	}
	if !sawThinkingEnd {
		t.Fatal("expected ThinkingEndEvent after Azure commentary normalization")
	}
	if done == nil || done.Message == nil || len(done.Message.Content) == 0 || done.Message.Content[0].Type != "thinking" {
		t.Fatalf("expected final thinking content, got %#v", done)
	}
}
