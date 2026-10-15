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
