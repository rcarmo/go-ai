package openairesponses

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0850ResponsesTerminalStatusClearsStaleErrorForSuccessAndLength(t *testing.T) {
	for _, tc := range []struct {
		name          string
		terminalEvent string
		wantStop      goai.StopReason
		wantRaw       string
	}{
		{
			name:          "completed clears prior incomplete error",
			terminalEvent: `data: {"type":"response.completed","response":{"id":"resp_2","status":"completed","usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			wantStop:      goai.StopReasonStop,
			wantRaw:       "completed",
		},
		{
			name:          "max output tokens clears prior incomplete error",
			terminalEvent: `data: {"type":"response.incomplete","response":{"id":"resp_2","status":"incomplete","incomplete_details":{"reason":"max_output_tokens"},"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
			wantStop:      goai.StopReasonLength,
			wantRaw:       "incomplete.max_output_tokens",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := strings.NewReader(strings.Join([]string{
				`data: {"type":"response.incomplete","response":{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"content_filter"},"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
				tc.terminalEvent,
			}, "\n\n") + "\n\n")
			done := collectV0850ResponsesDone(t, body)
			if done.StopReason != tc.wantStop || done.RawStopReason != tc.wantRaw || done.ErrorMessage != "" {
				t.Fatalf("message=%#v, want stop=%s raw=%s and no errorMessage", done, tc.wantStop, tc.wantRaw)
			}
		})
	}
}

func TestV0850ResponsesIncompleteErrorsRetainMappedErrorMessage(t *testing.T) {
	for _, reason := range []string{"content_filter", "unknown_reason"} {
		t.Run(reason, func(t *testing.T) {
			body := strings.NewReader(`data: {"type":"response.incomplete","response":{"id":"resp_1","status":"incomplete","incomplete_details":{"reason":"` + reason + `"},"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}` + "\n\n")
			done := collectV0850ResponsesDone(t, body)
			if done.StopReason != goai.StopReasonError || done.RawStopReason != "incomplete."+reason || !strings.Contains(done.ErrorMessage, reason) {
				t.Fatalf("message=%#v, want mapped incomplete error for %q", done, reason)
			}
		})
	}
}

func collectV0850ResponsesDone(t *testing.T, body *strings.Reader) *goai.Message {
	t.Helper()
	ch := make(chan goai.Event, 16)
	processStream(body, &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, Cost: goai.ModelCost{}}, ch)
	close(ch)
	var done *goai.Message
	for ev := range ch {
		if d, ok := ev.(*goai.DoneEvent); ok {
			done = d.Message
		}
	}
	if done == nil {
		t.Fatal("missing done event")
	}
	return done
}
