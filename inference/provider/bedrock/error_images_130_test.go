package bedrock

import (
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestBedrock130ClaudeErrorImagesAfterResults(t *testing.T) {
	model := upstreamBedrockModel()
	ctx := &goai.Context{Messages: []goai.Message{{Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, Content: []goai.ContentBlock{{Type: "toolCall", ID: "one", Name: "leaf", Arguments: map[string]interface{}{}}, {Type: "toolCall", ID: "two", Name: "leaf", Arguments: map[string]interface{}{}}}}, {Role: goai.RoleToolResult, ToolCallID: "one", IsError: true, Content: []goai.ContentBlock{{Type: "text", Text: "failed"}, {Type: "image", MimeType: "image/png", Data: "aW1hZ2U="}}}, {Role: goai.RoleToolResult, ToolCallID: "two", Content: []goai.ContentBlock{{Type: "image", MimeType: "image/png", Data: "aW1hZ2U="}}}}}
	messages := convertMessages(ctx,model,"none",nil)
	blocks := messages[len(messages)-1].Content
	if len(blocks) != 4 {
		t.Fatal(blocks)
	}
	first, ok := blocks[0].(*types.ContentBlockMemberToolResult)
	if !ok || first.Value.Status != types.ToolResultStatusError {
		t.Fatal(blocks[0])
	}
	for _, part := range first.Value.Content {
		if _, ok := part.(*types.ToolResultContentBlockMemberText); !ok {
			t.Fatal("error result image not extracted")
		}
	}
	second := blocks[1].(*types.ContentBlockMemberToolResult)
	if _, ok := second.Value.Content[1].(*types.ToolResultContentBlockMemberImage); !ok {
		t.Fatal("success image lost", second.Value.Content)
	}
	if _, ok := blocks[2].(*types.ContentBlockMemberText); !ok {
		t.Fatal("caption missing")
	}
	if _, ok := blocks[3].(*types.ContentBlockMemberImage); !ok {
		t.Fatal("image missing")
	}
}
