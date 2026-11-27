package anthropic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropicProviderStreamEventsForwardInOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}\n\n"))
		_, _ = w.Write([]byte("event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n"))
		_, _ = w.Write([]byte("event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n"))
		_, _ = w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer server.Close()

	model := &goai.Model{ID: "claude-test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages, BaseURL: server.URL, MaxTokens: 128}
	var gotTypes []string
	var gotModels []*goai.Model
	result, err := goai.Complete(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}, &goai.StreamOptions{
		APIKey: "test-key",
		OnProviderStreamEvent: func(value any, gotModel *goai.Model) error {
			payload, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("provider event type = %T, want map[string]any", value)
			}
			kind, _ := payload["type"].(string)
			gotTypes = append(gotTypes, kind)
			gotModels = append(gotModels, gotModel)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.StopReason != goai.StopReasonStop {
		t.Fatalf("stop reason = %q", result.StopReason)
	}
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if len(gotTypes) != len(want) {
		t.Fatalf("provider event types = %v, want %v", gotTypes, want)
	}
	for i := range want {
		if gotTypes[i] != want[i] {
			t.Fatalf("provider event %d = %q, want %q", i, gotTypes[i], want[i])
		}
		if gotModels[i] != model {
			t.Fatalf("provider event %d model = %p, want %p", i, gotModels[i], model)
		}
	}
}

func TestAnthropicReturnedModelIsSeparateFromRequestedModel(t *testing.T) {
	body := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"model\":\"proxy-model\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\n" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"thinking\",\"thinking\":\"reasoning\",\"signature\":\"signature\"}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	model := &goai.Model{ID: "claude-test", Provider: goai.ProviderAnthropic, Api: goai.ApiAnthropicMessages}
	ch := make(chan goai.Event, 8)
	processAnthropicStream(strings.NewReader(body), model, nil, "", goai.NewAssistantEventSender(ch))
	close(ch)
	for event := range ch {
		done, ok := event.(*goai.DoneEvent)
		if !ok {
			continue
		}
		if done.Message.Model != model.ID || done.Message.ResponseModel != "proxy-model" {
			t.Fatalf("model/responseModel = %q/%q, want %q/proxy-model", done.Message.Model, done.Message.ResponseModel, model.ID)
		}
		transformed := goai.TransformMessages([]goai.Message{*done.Message}, model)
		if len(transformed) != 1 || len(transformed[0].Content) != 1 || transformed[0].Content[0].Type != "thinking" || transformed[0].Content[0].ThinkingSignature != "signature" {
			t.Fatalf("replayed thinking = %#v", transformed)
		}
		return
	}
	t.Fatal("missing done event")
}
