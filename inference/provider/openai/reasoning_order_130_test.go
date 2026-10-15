package openai

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAI130CombinedReasoningPrecedesAnswer(t *testing.T) {
	for _, reasoning := range []string{`"reasoning_content":"think"`, `"reasoning":"think"`, `"reasoning_text":"think"`, `"reasoning_details":[{"type":"reasoning.text","text":"think"}]`} {
		stream := "data: {\"choices\":[{\"delta\":{\"content\":\"answer\"," + reasoning + "}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
		model := &goai.Model{Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, ID: "proxy"}
		var terminal *goai.Message
		processSSEStream(strings.NewReader(stream), model, func(event goai.Event) {
			if done, ok := event.(*goai.DoneEvent); ok {
				terminal = done.Message
			}
		})
		if terminal == nil || len(terminal.Content) != 2 || terminal.Content[0].Type != "thinking" || terminal.Content[1].Type != "text" || terminal.Content[1].Text != "answer" {
			t.Fatal(reasoning, terminal)
		}
	}
}
