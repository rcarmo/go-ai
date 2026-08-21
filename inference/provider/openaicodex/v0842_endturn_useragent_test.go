package openaicodex

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0842CodexSSECapturesEndTurn(t *testing.T) {
	model := &goai.Model{ID: "gpt-5.4-codex", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses}
	stream := strings.Join([]string{
		`data: {"type":"response.completed","response":{"id":"resp_1","status":"completed","end_turn":false,"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}}`,
		`data: [DONE]`,
	}, "\n\n")
	ch := make(chan goai.Event, 8)
	processCodexSSE(strings.NewReader(stream), model, ch, nil)
	close(ch)
	var done *goai.Message
	for event := range ch {
		if e, ok := event.(*goai.DoneEvent); ok {
			done = e.Message
		}
	}
	if done == nil || done.EndTurn == nil || *done.EndTurn {
		t.Fatalf("expected endTurn=false, got %#v", done)
	}
}

func TestV0842CodexUserAgentMatchesPiShape(t *testing.T) {
	h := buildCodexSSEHeaders(nil, nil, nil, "acct", "token", "session")
	if h.Get("User-Agent") == "" || !strings.HasPrefix(h.Get("User-Agent"), "pi (") || strings.HasPrefix(h.Get("User-Agent"), "go-ai (") {
		t.Fatalf("unexpected user-agent: %q", h.Get("User-Agent"))
	}
}
