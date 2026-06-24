package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func thinkingAsTextCompat() goai.OpenAICompletionsCompat {
	requires := true
	return goai.OpenAICompletionsCompat{RequiresThinkingAsText: &requires, SupportsStore: boolPtr(false)}
}

func thinkingAsTextModel(baseURL string) *goai.Model {
	compat := thinkingAsTextCompat()
	return &goai.Model{ID: "repro-model", Provider: "repro-provider", Api: goai.ApiOpenAICompletions, BaseURL: baseURL, Reasoning: true, CompletionsCompat: &compat}
}

func thinkingAsTextAssistant(content []goai.ContentBlock) goai.Message {
	return goai.Message{Role: goai.RoleAssistant, Content: content, Api: goai.ApiOpenAICompletions, Provider: "repro-provider", Model: "repro-model", Usage: &goai.Usage{}, StopReason: goai.StopReasonStop}
}

func thinkingAsTextContext(assistant goai.Message) *goai.Context {
	return &goai.Context{Messages: []goai.Message{goai.UserMessage("hello"), assistant, goai.UserMessage("continue")}}
}

func TestOpenAICompletionsThinkingAsTextSerializesThinkingPlusTextReplayAsAssistantTextParts(t *testing.T) {
	compat := thinkingAsTextCompat()
	messages := convertMessages(thinkingAsTextModel("http://127.0.0.1:1"), thinkingAsTextContext(thinkingAsTextAssistant([]goai.ContentBlock{{Type: "thinking", Thinking: "internal reasoning"}, {Type: "text", Text: "visible answer"}})), &compat)
	want := []map[string]any{{"type": "text", "text": "internal reasoning"}, {"type": "text", "text": "visible answer"}}
	got, ok := messages[1].Content.([]map[string]any)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("assistant content=%#v (%T), want %#v", messages[1].Content, messages[1].Content, want)
	}
}

func TestOpenAICompletionsThinkingAsTextSerializesThinkingOnlyReplayAsAssistantTextParts(t *testing.T) {
	compat := thinkingAsTextCompat()
	messages := convertMessages(thinkingAsTextModel("http://127.0.0.1:1"), thinkingAsTextContext(thinkingAsTextAssistant([]goai.ContentBlock{{Type: "thinking", Thinking: "internal reasoning"}})), &compat)
	want := []map[string]any{{"type": "text", "text": "internal reasoning"}}
	got, ok := messages[1].Content.([]map[string]any)
	if !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("assistant content=%#v (%T), want %#v", messages[1].Content, messages[1].Content, want)
	}
}

func TestOpenAICompletionsThinkingAsTextReachesEndpointWithThinkingAndTextReplay(t *testing.T) {
	var requestBody struct {
		Messages []chatMessage `json:"messages"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/chat/completions" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&requestBody); err != nil {
			t.Fatal(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-repro\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"repro-model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"ok\"},\"finish_reason\":null}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"id\":\"chatcmpl-repro\",\"object\":\"chat.completion.chunk\",\"created\":0,\"model\":\"repro-model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	var done bool
	for ev := range streamOpenAI(context.Background(), thinkingAsTextModel(server.URL), thinkingAsTextContext(thinkingAsTextAssistant([]goai.ContentBlock{{Type: "thinking", Thinking: "internal reasoning"}, {Type: "text", Text: "visible answer"}})), &goai.StreamOptions{APIKey: "test-key"}) {
		switch e := ev.(type) {
		case *goai.DoneEvent:
			done = true
		case *goai.ErrorEvent:
			t.Fatalf("unexpected error: %v", e.Err)
		}
	}
	if !done {
		t.Fatal("expected done event")
	}
	want := []map[string]any{{"type": "text", "text": "internal reasoning"}, {"type": "text", "text": "visible answer"}}
	got, ok := requestBody.Messages[1].Content.([]any)
	if !ok {
		t.Fatalf("assistant request content=%#v (%T), want array", requestBody.Messages[1].Content, requestBody.Messages[1].Content)
	}
	var normalized []map[string]any
	for _, item := range got {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("assistant request content item=%#v", item)
		}
		normalized = append(normalized, m)
	}
	if !reflect.DeepEqual(normalized, want) {
		t.Fatalf("assistant request content=%#v, want %#v", normalized, want)
	}
}

func boolPtr(v bool) *bool { return &v }
