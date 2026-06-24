package bedrock

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
)

func upstreamBedrockModel() *goai.Model {
	return &goai.Model{ID: "us.anthropic.claude-sonnet-4-5-20250929-v1:0", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, Input: []string{"text", "image"}}
}

func bedrockTextValue(t *testing.T, block types.ContentBlock) string {
	t.Helper()
	text, ok := block.(*types.ContentBlockMemberText)
	if !ok {
		t.Fatalf("expected text content block, got %T", block)
	}
	return text.Value
}

func bedrockToolResultTextValue(t *testing.T, block types.ContentBlock) string {
	t.Helper()
	tr, ok := block.(*types.ContentBlockMemberToolResult)
	if !ok {
		t.Fatalf("expected toolResult content block, got %T", block)
	}
	if len(tr.Value.Content) != 1 {
		t.Fatalf("expected one toolResult content block, got %d", len(tr.Value.Content))
	}
	text, ok := tr.Value.Content[0].(*types.ToolResultContentBlockMemberText)
	if !ok {
		t.Fatalf("expected text toolResult content block, got %T", tr.Value.Content[0])
	}
	return text.Value
}

func TestUpstreamBedrockConvertMessagesUnknownAndEmptyContent(t *testing.T) {
	model := upstreamBedrockModel()
	tests := []struct {
		name      string
		messages  []goai.Message
		wantRoles []types.ConversationRole
		wantTexts []string
	}{
		{
			name: "skips unknown user content blocks instead of throwing",
			messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{
				{Type: "text", Text: "hello"},
				{Type: "unknown", Text: "foo"},
			}}},
			wantRoles: []types.ConversationRole{types.ConversationRoleUser},
			wantTexts: []string{"hello"},
		},
		{
			name: "skips unknown assistant content blocks instead of throwing",
			messages: []goai.Message{{Role: goai.RoleAssistant, Content: []goai.ContentBlock{
				{Type: "text", Text: "hello"},
				{Type: "unknown", Text: "foo"},
			}}},
			wantRoles: []types.ConversationRole{types.ConversationRoleAssistant},
			wantTexts: []string{"hello"},
		},
		{
			name:      "replaces user messages with only unknown content blocks with a placeholder",
			messages:  []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "unknown", Text: "foo"}}}},
			wantRoles: []types.ConversationRole{types.ConversationRoleUser},
			wantTexts: []string{"<empty>"},
		},
		{
			name:      "replaces blank user string content with a placeholder",
			messages:  []goai.Message{goai.UserMessage("   ")},
			wantRoles: []types.ConversationRole{types.ConversationRoleUser},
			wantTexts: []string{"<empty>"},
		},
		{
			name: "filters blank user text blocks when other content remains",
			messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{
				{Type: "text", Text: ""},
				{Type: "text", Text: "hello"},
			}}},
			wantRoles: []types.ConversationRole{types.ConversationRoleUser},
			wantTexts: []string{"hello"},
		},
		{
			name:      "replaces user content emptied by surrogate sanitization with a placeholder",
			messages:  []goai.Message{goai.UserMessage("\xed\xa0\xbd")},
			wantRoles: []types.ConversationRole{types.ConversationRoleUser},
			wantTexts: []string{"<empty>"},
		},
		{
			name:      "skips assistant text blocks emptied by surrogate sanitization",
			messages:  []goai.Message{{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "text", Text: "\xed\xa0\xbd"}}}},
			wantRoles: nil,
			wantTexts: nil,
		},
		{
			name:      "skips assistant messages with only unknown content blocks",
			messages:  []goai.Message{{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "unknown", Text: "foo"}}}},
			wantRoles: nil,
			wantTexts: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msgs := convertMessages(&goai.Context{Messages: tt.messages}, model, "none", nil)
			if len(msgs) != len(tt.wantRoles) {
				t.Fatalf("got %d messages, want %d: %#v", len(msgs), len(tt.wantRoles), msgs)
			}
			for i, wantRole := range tt.wantRoles {
				if msgs[i].Role != wantRole {
					t.Fatalf("message %d role = %v, want %v", i, msgs[i].Role, wantRole)
				}
				if len(msgs[i].Content) != 1 {
					t.Fatalf("message %d content len = %d, want 1", i, len(msgs[i].Content))
				}
				if got := bedrockTextValue(t, msgs[i].Content[0]); got != tt.wantTexts[i] {
					t.Fatalf("message %d text = %q, want %q", i, got, tt.wantTexts[i])
				}
			}
		})
	}
}

func TestUpstreamBedrockConvertMessagesBlankToolResultPlaceholder(t *testing.T) {
	msgs := convertMessages(&goai.Context{Messages: []goai.Message{{
		Role:       goai.RoleToolResult,
		ToolCallID: "tool-1",
		ToolName:   "tool",
		Content:    []goai.ContentBlock{{Type: "text", Text: ""}},
	}}}, upstreamBedrockModel(), "none", nil)
	if len(msgs) != 1 || msgs[0].Role != types.ConversationRoleUser || len(msgs[0].Content) != 1 {
		t.Fatalf("unexpected converted messages: %#v", msgs)
	}
	if got := bedrockToolResultTextValue(t, msgs[0].Content[0]); got != "<empty>" {
		t.Fatalf("tool result text = %q, want <empty>", got)
	}
}
