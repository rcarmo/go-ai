package openairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0991OpenAIResponsesProviderStreamEventSeesRawEventBeforeAzureNormalizationAndFailureTerminates(t *testing.T) {
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses}
	body := strings.NewReader(strings.Join([]string{
		`data: {"type":"response.output_item.added","item":{"type":"message","id":"item_1","phase":"commentary"}}`,
		`data: {"type":"response.reasoning_text.delta","delta":"hidden"}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
	}, "\n\n") + "\n\n")
	wantErr := errors.New("raw hook rejected")
	var seen []string
	ch := make(chan goai.Event, 16)
	processStreamWithOptions(body, model, &goai.StreamOptions{OnProviderStreamEvent: func(event interface{}, gotModel *goai.Model) error {
		if gotModel != model {
			t.Fatalf("hook model mismatch: %#v", gotModel)
		}
		raw, ok := event.(map[string]interface{})
		if !ok {
			t.Fatalf("hook event type = %T", event)
		}
		typ, _ := raw["type"].(string)
		seen = append(seen, typ)
		if typ == "response.reasoning_text.delta" {
			return wantErr
		}
		return nil
	}}, goai.NewAssistantEventSender(ch))
	close(ch)

	var gotErr *goai.ErrorEvent
	for ev := range ch {
		switch e := ev.(type) {
		case *goai.ErrorEvent:
			gotErr = e
		case *goai.DoneEvent:
			t.Fatalf("unexpected done after hook failure: %#v", e)
		}
	}
	if strings.Join(seen, ",") != "response.output_item.added,response.reasoning_text.delta" {
		t.Fatalf("raw hook saw %q", seen)
	}
	if gotErr == nil || !errors.Is(gotErr.Err, wantErr) || gotErr.Error == nil || gotErr.Error.StopReason != goai.StopReasonPending {
		t.Fatalf("unexpected error event: %#v", gotErr)
	}
}

func TestV0991OpenAIResponsesDirectChatGPTTokenSuppressesUnsupportedFields(t *testing.T) {
	temp := 0.7
	max := 128
	yes := true
	model := &goai.Model{
		ID:        "gpt-5.4",
		Provider:  goai.ProviderOpenAI,
		Api:       goai.ApiOpenAIResponses,
		BaseURL:   "https://api.openai.com/v1",
		MaxTokens: 4096,
		ResponsesCompat: &goai.OpenAIResponsesCompat{
			SupportsExplicitPromptCacheMode: &yes,
			SupportsLongCacheRetention:      &yes,
		},
	}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{APIKey: "chatgpt-user-access-token", SessionID: "session-1", CacheRetention: goai.CacheRetentionLong, Temperature: &temp, MaxTokens: &max})
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"temperature", "max_output_tokens", "prompt_cache_retention", "prompt_cache_options"} {
		if _, ok := payload[field]; ok {
			t.Fatalf("%s should be suppressed for direct ChatGPT token: %s", field, body)
		}
	}
	if payload["prompt_cache_key"] != "session-1" || payload["store"] != false || payload["stream"] != true {
		t.Fatalf("unexpected retained fields: %s", body)
	}
}

func TestV0991OpenAIResponsesUsageLimitGuidanceForHTTPAndSSEErrors(t *testing.T) {
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}
	limitBody := `{"error":{"code":"subscription_sharing_usage_limit_exceeded","message":"limit"}}`

	t.Run("http", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(limitBody))
		}))
		defer server.Close()
		m := *model
		m.BaseURL = server.URL
		var gotErr error
		for ev := range streamResponses(context.Background(), &m, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{APIKey: "sk-test"}) {
			if e, ok := ev.(*goai.ErrorEvent); ok {
				gotErr = e.Err
			}
		}
		if gotErr == nil || !strings.Contains(gotErr.Error(), "subscription_sharing_usage_limit_exceeded") || !strings.Contains(gotErr.Error(), chatGPTUsageURL) {
			t.Fatalf("missing usage guidance: %v", gotErr)
		}
	})

	t.Run("sse error", func(t *testing.T) {
		body := strings.NewReader(`data: {"type":"error","code":"subscription_sharing_usage_limit_exceeded","message":"subscription_sharing_usage_limit_exceeded: limit"}` + "\n\n")
		ch := make(chan goai.Event, 8)
		processStreamWithOptions(body, model, nil, goai.NewAssistantEventSender(ch))
		close(ch)
		assertUsageLimitError(t, ch)
	})

	t.Run("sse response failed", func(t *testing.T) {
		body := strings.NewReader(`data: {"type":"response.failed","response":{"status":"failed","error":{"code":"subscription_sharing_usage_limit_exceeded","message":"limit"}}}` + "\n\n")
		ch := make(chan goai.Event, 8)
		processStreamWithOptions(body, model, nil, goai.NewAssistantEventSender(ch))
		close(ch)
		assertUsageLimitError(t, ch)
	})
}

func assertUsageLimitError(t *testing.T, ch <-chan goai.Event) {
	t.Helper()
	for ev := range ch {
		if e, ok := ev.(*goai.ErrorEvent); ok {
			text := fmt.Sprint(e.Err)
			if e.Error != nil && e.Error.ErrorMessage != "" {
				text += " " + e.Error.ErrorMessage
			}
			if strings.Contains(text, "subscription_sharing_usage_limit_exceeded") && strings.Contains(text, chatGPTUsageURL) {
				return
			}
			t.Fatalf("missing usage guidance: %#v", e)
		}
	}
	t.Fatal("expected error event")
}
