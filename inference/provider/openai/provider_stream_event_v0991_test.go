package openai

import (
	"errors"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0991OpenAICompletionsProviderStreamEventHookBeforeNormalizationAndFailureTerminates(t *testing.T) {
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions}
	body := strings.NewReader("data: {\"id\":\"chatcmpl_1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n" +
		"data: {\"id\":\"chatcmpl_1\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\" after\"},\"finish_reason\":\"stop\"}]}\n\n")
	wantErr := errors.New("stop from hook")
	var seen []string
	ch := make(chan goai.Event, 16)
	processSSEStreamWithOptions(body, model, &goai.StreamOptions{OnProviderStreamEvent: func(event interface{}, gotModel *goai.Model) error {
		if gotModel != model {
			t.Fatalf("hook model mismatch: %#v", gotModel)
		}
		chunk, ok := event.(sseChunk)
		if !ok {
			t.Fatalf("hook event type = %T, want sseChunk", event)
		}
		if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != nil {
			seen = append(seen, *chunk.Choices[0].Delta.Content)
		}
		if len(seen) == 2 {
			return wantErr
		}
		return nil
	}}, goai.NewAssistantEventSender(ch))
	close(ch)

	var text strings.Builder
	var gotErr *goai.ErrorEvent
	for ev := range ch {
		switch e := ev.(type) {
		case *goai.TextDeltaEvent:
			text.WriteString(e.Delta)
		case *goai.ErrorEvent:
			gotErr = e
		case *goai.DoneEvent:
			t.Fatalf("unexpected done after hook failure: %#v", e)
		}
	}
	if strings.Join(seen, "|") != "hello| after" {
		t.Fatalf("hook saw chunks %q", seen)
	}
	if gotErr == nil || !errors.Is(gotErr.Err, wantErr) || gotErr.Error == nil || gotErr.Error.StopReason != goai.StopReasonPending {
		t.Fatalf("unexpected error event: %#v", gotErr)
	}
	if text.String() != "hello" {
		t.Fatalf("normalized text after hook failure = %q, want only first delta", text.String())
	}
}
