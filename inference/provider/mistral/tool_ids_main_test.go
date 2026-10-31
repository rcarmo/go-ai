package mistral

import (
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

// Sequential vectors from the pinned normalizer cover both collision paths,
// repeat identity, the normalized hash seed and UTF-16 hashing.
func TestMistralToolIDSourceMain(t *testing.T) {
	normalize := newMistralToolCallIDNormalizer()
	for _, tc := range []struct{ id, want string }{{"abcdefghi", "abcdefghi"}, {"a-bcdefghi", "mlj703uel"}, {"abcdefghi", "abcdefghi"}, {"short-id", "e8bx2r1wt"}, {"short_id", "1x5bm861p"}, {"😃", "44oeks3bc"}, {"", "k4n83c7h0"}, {"Tool_0001", "100gi3i3u"}} {
		if got := normalize(tc.id); got != tc.want {
			t.Fatal("source ID mismatch", tc.id, got, tc.want)
		}
	}
}

func TestMistralToolIDsRequestMappingMain(t *testing.T) {
	model := &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}
	ctx := &goai.Context{Messages: []goai.Message{
		{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, Content: []goai.ContentBlock{{Type: "toolCall", ID: "abcdefghi", Name: "first", Arguments: map[string]any{}}, {Type: "toolCall", ID: "a-bcdefghi", Name: "second", Arguments: map[string]any{}}}},
		{Role: goai.RoleToolResult, ToolCallID: "abcdefghi", ToolName: "first", Content: []goai.ContentBlock{{Type: "text", Text: "one"}}},
		{Role: goai.RoleToolResult, ToolCallID: "a-bcdefghi", ToolName: "second", Content: []goai.ContentBlock{{Type: "text", Text: "two"}}},
	}}
	for attempt := 0; attempt < 2; attempt++ {
		request := buildRequest(model, ctx, &goai.StreamOptions{})
		if len(request.Messages) != 3 || len(request.Messages[0].ToolCalls) != 2 {
			t.Fatal("transcript shape changed", request.Messages)
		}
		first, second := request.Messages[0].ToolCalls[0].ID, request.Messages[0].ToolCalls[1].ID
		if first != "abcdefghi" || second != "mlj703uel" || request.Messages[1].ToolCallID != first || request.Messages[2].ToolCallID != second {
			t.Fatal("tool result routed to wrong call", request.Messages)
		}
	}
	if ctx.Messages[0].Content[1].ID != "a-bcdefghi" || ctx.Messages[2].ToolCallID != "a-bcdefghi" {
		t.Fatal("request mapping mutated context")
	}
}

func TestMistralFirstResponseIDMain(t *testing.T) {
	wire := "data: {\"id\":\"\",\"choices\":[]}\n\n" + "data: {\"id\":\"first\",\"choices\":[],\"usage\":{\"prompt_tokens\":2}}\n\n" + "data: {\"id\":\"later\",\"choices\":[{\"delta\":{\"content\":\"hi\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"
	var done *goai.Message
	processSSEStream(strings.NewReader(wire), &goai.Model{ID: "test", Provider: goai.ProviderMistral, Api: goai.ApiMistralConversations}, func(event goai.Event) {
		switch e := event.(type) {
		case *goai.TextDeltaEvent:
			if e.Partial.ResponseID != "first" {
				t.Fatal("response ID missing/overwritten during stream", e.Partial.ResponseID)
			}
		case *goai.DoneEvent:
			done = e.Message
		}
	})
	if done == nil || done.ResponseID != "first" {
		t.Fatal("first ID lost", done)
	}
}
