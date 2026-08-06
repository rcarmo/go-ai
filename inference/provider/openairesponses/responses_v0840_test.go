package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestResponsesIncompleteTerminalReasonMapping(t *testing.T) {
	for _, tc := range []struct {
		name      string
		reason    string
		wantStop  goai.StopReason
		wantError bool
	}{
		{"length", "max_output_tokens", goai.StopReasonLength, false},
		{"other", "content_filter", goai.StopReasonError, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.NewReader(`data: {"type":"response.incomplete","response":{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"` + tc.reason + `"},"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n")
			ch := make(chan goai.Event, 10)
			processStream(body, &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses}, ch)
			close(ch)
			var done *goai.DoneEvent
			for ev := range ch {
				if d, ok := ev.(*goai.DoneEvent); ok {
					done = d
				}
			}
			if done == nil || done.Message.StopReason != tc.wantStop || done.Message.RawStopReason != "incomplete."+tc.reason {
				t.Fatalf("done=%#v", done)
			}
			if tc.wantError && !strings.Contains(done.Message.ErrorMessage, tc.reason) {
				t.Fatalf("missing incomplete error reason: %#v", done.Message)
			}
		})
	}
}

func TestResponsesSamplingParamsOverrideTypedFields(t *testing.T) {
	temp := 0.2
	max := 64
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, BaseURL: "https://api.openai.com/v1", MaxTokens: 1000, SamplingParams: map[string]any{"top_p": 0.9, "temperature": 0.7}}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{Temperature: &temp, MaxTokens: &max, SamplingParams: map[string]any{"top_k": float64(40), "temperature": 0.8}})
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["temperature"] != 0.8 || payload["top_p"] != 0.9 || payload["top_k"] != float64(40) || payload["max_output_tokens"] != float64(max) {
		t.Fatalf("sampling params did not merge/override last: %s", body)
	}
}
