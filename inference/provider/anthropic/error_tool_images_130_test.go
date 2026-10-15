package anthropic

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropic130ErrorImagesFollowAllToolResults(t *testing.T) {
	model := opusDeferredModel()
	ctx := deferredContext([]goai.Tool{deferredTool("base_tool")}, nil)
	ctx.Messages[1].Content = append(ctx.Messages[1].Content, goai.ContentBlock{Type: "toolCall", ID: "call_2", Name: "base_tool", Arguments: map[string]interface{}{}})
	ctx.Messages[2].IsError = true
	ctx.Messages[2].Content = []goai.ContentBlock{{Type: "text", Text: "failed"}, {Type: "image", MimeType: "image/png", Data: "Zmlyc3Q="}, {Type: "image", MimeType: "image/png", Data: "c2Vjb25k"}, {Type: "text", Text: "tail"}}
	second := goai.Message{Role: goai.RoleToolResult, ToolCallID: "call_2", ToolName: "base_tool", Content: []goai.ContentBlock{{Type: "image", MimeType: "image/png", Data: "dGhpcmQ="}}}
	ctx.Messages = append(ctx.Messages[:3], append([]goai.Message{second}, ctx.Messages[3:]...)...)
	before, _ := json.Marshal(ctx)
	req := buildRequest(model, ctx, &goai.StreamOptions{})
	after, _ := json.Marshal(ctx)
	if string(before) != string(after) {
		t.Fatal("caller transcript mutated")
	}
	blocks := req.Messages[2].Content.([]anthropicContentBlock)
	types := []string{}
	for _, b := range blocks {
		types = append(types, b.Type)
	}
	if !reflect.DeepEqual(types, []string{"tool_result", "tool_result", "text", "image", "image"}) {
		t.Fatal(types)
	}
	if !blocks[0].IsError {
		t.Fatal("error flag lost")
	}
	errorContent := blocks[0].Content.([]anthropicContentBlock)
	for _, b := range errorContent {
		if b.Type != "text" {
			t.Fatal("nontext error result", b)
		}
	}
	if len(errorContent) != 3 || errorContent[1].Text != "(image attached after the tool results)" || !strings.Contains(blocks[2].Text, "call_1") {
		t.Fatal(errorContent, blocks[2])
	}
	success := blocks[1].Content.([]anthropicContentBlock)
	if len(success) != 1 || success[0].Type != "image" || success[0].Source.Data != "dGhpcmQ=" {
		t.Fatal("successful result image lost", success)
	}
	if blocks[3].Source.Data != "Zmlyc3Q=" || blocks[4].Source.Data != "c2Vjb25k" {
		t.Fatal("extracted order lost")
	}
}
