package openaicodex

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0850CodexProcessesTerminalSSEWithoutTrailingBlankLine(t *testing.T) {
	model := &goai.Model{ID: "gpt-5.4-codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses, Cost: goai.ModelCost{}}
	stream := strings.Join([]string{
		`data: {"type":"response.output_item.added","item":{"type":"message","id":"msg_1","role":"assistant","status":"in_progress","content":[]}}`,
		`data: {"type":"response.content_part.added","part":{"type":"output_text","text":""}}`,
		`data: {"type":"response.output_text.delta","delta":"Hello"}`,
		`data: {"type":"response.output_item.done","item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8,"input_tokens_details":{"cached_tokens":0}}}}`,
	}, "\n\n")

	ch := make(chan goai.Event, 16)
	processCodexSSE(strings.NewReader(stream), model, ch, nil)
	close(ch)
	var done *goai.Message
	for event := range ch {
		if e, ok := event.(*goai.DoneEvent); ok {
			done = e.Message
		}
	}
	if done == nil || done.StopReason != goai.StopReasonStop || extractCodexTextV0850(done.Content) != "Hello" {
		t.Fatalf("done=%#v, want stop text Hello", done)
	}
}

func extractCodexTextV0850(blocks []goai.ContentBlock) string {
	for _, block := range blocks {
		if block.Type == "text" {
			return block.Text
		}
	}
	return ""
}
