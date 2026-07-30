package openai

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestOpenAICompletionsMalformedToolDeltaFunctionWithEmptyCustomPreservesFunctionArguments(t *testing.T) {
	body := strings.NewReader(`data: {"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"hi\"}"},"custom":{}}]}}]}

` +
		`data: {"choices":[{"index":0,"finish_reason":"tool_calls","delta":{}}]}

`)
	ch := make(chan goai.Event, 20)
	processSSEStream(body, &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions}, ch)
	close(ch)
	for ev := range ch {
		if done, ok := ev.(*goai.DoneEvent); ok {
			if len(done.Message.Content) != 1 || done.Message.Content[0].Arguments["q"] != "hi" {
				t.Fatalf("message=%#v", done.Message)
			}
			return
		}
	}
	t.Fatal("missing done")
}
