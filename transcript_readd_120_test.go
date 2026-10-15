package goai

import (
	"strings"
	"testing"
)

func TestTranscript120ToolRemovalReaddHasSingleOrderedDeclaration(t *testing.T) {
	messages := []Message{{Role: RoleSystem, ToolsAdded: []Tool{{Name: "a", Description: "first"}, {Name: "b"}}}, {Role: RoleSystem, ToolsRemoved: []ToolReference{{Name: "a"}}}, {Role: RoleSystem, ToolsAdded: []Tool{{Name: "a", Description: "second"}}}, {Role: RoleSystem, ToolsAdded: []Tool{{Name: "b", Description: "updated"}}}}
	tools := currentTranscriptTools(messages)
	if len(tools) != 2 || tools[0].Name != "b" || tools[0].Description != "updated" || tools[1].Name != "a" || tools[1].Description != "second" {
		t.Fatal(tools)
	}
	for _, mid := range []bool{false, true} {
		context := ResolveContext(&Context{Messages: messages}, mid)
		if len(context.Tools) != 2 || context.Tools[0].Name != "b" || context.Tools[1].Name != "a" {
			t.Fatal(mid, context.Tools)
		}
	}
}
func TestTranscript120SectionRemovalReaddRendersOnce(t *testing.T) {
	first, second, kept := "first", "second", "kept"
	messages := []Message{{Role: RoleSystem, Sections: map[string]*string{"a": &first}}, {Role: RoleSystem, Sections: map[string]*string{"b": &kept}}, {Role: RoleSystem, Sections: map[string]*string{"a": nil}}, {Role: RoleSystem, Sections: map[string]*string{"a": &second}}}
	text := currentTranscriptSystemText(messages)
	if strings.Count(text, "<a>") != 1 || strings.Contains(text, "first") || strings.Index(text, "<b>") > strings.Index(text, "<a>") {
		t.Fatal(text)
	}
	context := ResolveContext(&Context{Messages: messages}, false)
	if context.SystemPrompt != text {
		t.Fatal(context.SystemPrompt, text)
	}
}

func TestTranscript120CollapsePreservesInitialTimestampAndEmptySystem(t *testing.T) {
	for _, empty := range []bool{false, true} {
		messages := []Message{{Role: RoleUser, Content: []ContentBlock{{Type: "text", Text: "hello"}}}, {Role: RoleSystem, Timestamp: 123}, {Role: RoleSystem, Timestamp: 456}}
		if !empty {
			messages[1].Content = []ContentBlock{{Type: "text", Text: "first"}}
			messages[2].Content = []ContentBlock{{Type: "text", Text: "later"}}
		}
		collapsed := ResolveTranscript(&Context{Messages: messages}, false)
		if len(collapsed.Messages) != 2 || collapsed.Messages[0].Role != RoleSystem || collapsed.Messages[0].Timestamp != 123 || collapsed.Messages[1].Role != RoleUser {
			t.Fatal(empty, collapsed)
		}
		if !empty && ContentText(collapsed.Messages[0].Content) != "first\n\nlater" {
			t.Fatal(collapsed)
		}
	}
}
func TestTranscript120SectionPatchRenderingStable(t *testing.T) {
	a, b := "one", "two"
	message := Message{Role: RoleSystem, Sections: map[string]*string{"z": nil, "b": &b, "a": &a}}
	for i := 0; i < 100; i++ {
		rendered := RenderSystemMessageText(message)
		if strings.Index(rendered, `section "a"`) > strings.Index(rendered, `section "b"`) || strings.Index(rendered, `section "b"`) > strings.Index(rendered, `section "z"`) {
			t.Fatal(rendered)
		}
		prompt := currentTranscriptSystemText([]Message{message})
		if strings.Index(prompt, "<a>") > strings.Index(prompt, "<b>") {
			t.Fatal(prompt)
		}
	}
}
