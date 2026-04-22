package bedrock

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
)

func TestExtractRegionFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://bedrock-runtime.us-east-1.amazonaws.com", "us-east-1"},
		{"https://bedrock-runtime.eu-west-1.amazonaws.com", "eu-west-1"},
		{"https://example.com", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := extractRegionFromURL(tt.url); got != tt.want {
			t.Fatalf("extractRegionFromURL(%q) = %q, want %q", tt.url, got, tt.want)
		}
	}
}

func TestBuildConverseInputIncludesSystemToolsAndThinking(t *testing.T) {
	level := goai.ThinkingHigh
	customBudget := 7777
	model := &goai.Model{ID: "anthropic.claude-3-7-sonnet", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, Reasoning: true}
	ctx := &goai.Context{
		SystemPrompt: "You are helpful.",
		Messages: []goai.Message{goai.UserMessage("hello")},
		Tools: []goai.Tool{{Name: "search", Description: "Search docs", Parameters: []byte(`{"type":"object"}`)}},
	}
	opts := &goai.StreamOptions{
		MaxTokens: &[]int{1234}[0],
		Reasoning: &level,
		ThinkingBudgets: &goai.ThinkingBudgets{High: &customBudget},
	}

	input := buildConverseInput(model, ctx, opts)
	if aws.ToString(input.ModelId) != model.ID {
		t.Fatalf("unexpected model id: %q", aws.ToString(input.ModelId))
	}
	if len(input.System) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(input.System))
	}
	if len(input.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(input.Messages))
	}
	if input.InferenceConfig == nil || aws.ToInt32(input.InferenceConfig.MaxTokens) != 1234 {
		t.Fatal("expected inference config max tokens")
	}
	if input.ToolConfig == nil || len(input.ToolConfig.Tools) != 1 {
		t.Fatal("expected one tool in tool config")
	}
	if input.AdditionalModelRequestFields == nil {
		t.Fatal("expected additional model request fields for thinking config")
	}
}

func TestConvertMessagesCoalescesConsecutiveToolResults(t *testing.T) {
	model := &goai.Model{ID: "anthropic.claude-3-7-sonnet", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, Input: []string{"text"}}
	ctx := &goai.Context{Messages: []goai.Message{
		goai.UserMessage("start"),
		{Role: goai.RoleToolResult, ToolCallID: "tc1", ToolName: "a", Content: []goai.ContentBlock{{Type: "text", Text: "one"}}},
		{Role: goai.RoleToolResult, ToolCallID: "tc2", ToolName: "b", Content: []goai.ContentBlock{{Type: "text", Text: "two"}}, IsError: true},
	}}

	msgs := convertMessages(ctx, model)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 bedrock messages, got %d", len(msgs))
	}
	if msgs[1].Role != types.ConversationRoleUser {
		t.Fatalf("expected tool results to become user message, got %v", msgs[1].Role)
	}
	if len(msgs[1].Content) != 2 {
		t.Fatalf("expected 2 tool result blocks, got %d", len(msgs[1].Content))
	}
}

func TestCreateImageBlockDecodesBase64(t *testing.T) {
	block := createImageBlock("image/png", "aGVsbG8=")
	img, ok := block.(*types.ContentBlockMemberImage)
	if !ok {
		t.Fatalf("expected image block, got %T", block)
	}
	if img.Value.Format != types.ImageFormatPng {
		t.Fatalf("expected png format, got %v", img.Value.Format)
	}
}

func TestBedrockPayloadHookCanReplaceInput(t *testing.T) {
	orig := &bedrockruntime.ConverseStreamInput{ModelId: aws.String("a")}
	replaced := &bedrockruntime.ConverseStreamInput{ModelId: aws.String("b")}
	payload, err := goai.InvokeOnPayload(&goai.StreamOptions{OnPayload: func(payload interface{}, model *goai.Model) (interface{}, error) {
		return replaced, nil
	}}, orig, &goai.Model{})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := payload.(*bedrockruntime.ConverseStreamInput)
	if !ok || aws.ToString(got.ModelId) != "b" {
		t.Fatalf("expected replaced input, got %#v", payload)
	}
}
