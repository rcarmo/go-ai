package goai_test

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestV0870ResolveTranscriptFoldsSystemSectionsAndToolsForLegacyProviders(t *testing.T) {
	base := goai.Tool{Name: "base_tool", Description: "base"}
	late := goai.Tool{Name: "late_tool", Description: "late"}
	oldRules := "old rules"
	newRules := "new rules"
	remove := (*string)(nil)
	ctx := &goai.Context{Messages: []goai.Message{
		{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "base prompt"}}, Sections: map[string]*string{"rules": &oldRules}, ToolsAdded: []goai.Tool{base}},
		goai.UserMessage("before"),
		{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "updated guidance"}}, Sections: map[string]*string{"rules": &newRules, "docs": remove}, ToolsRemoved: []goai.ToolReference{{Name: "base_tool"}}, ToolsAdded: []goai.Tool{late}},
	}}

	resolved := goai.ResolveTranscript(ctx, false)
	if len(resolved.Messages) != 2 || resolved.Messages[0].Role != goai.RoleSystem || resolved.Messages[1].Role != goai.RoleUser {
		t.Fatalf("resolved messages=%#v", resolved.Messages)
	}
	if got := resolved.Messages[0].Content[0].Text; got != "base prompt\n\nupdated guidance\n\n<rules>\nnew rules\n</rules>" {
		t.Fatalf("system text=%q", got)
	}
	if !reflect.DeepEqual(resolved.Messages[0].ToolsAdded, []goai.Tool{late}) {
		t.Fatalf("tools=%#v", resolved.Messages[0].ToolsAdded)
	}
}

func TestV0870ResolveTranscriptPreservesNativeSystemUpdates(t *testing.T) {
	ctx := &goai.Context{SystemPrompt: "legacy prompt", Tools: []goai.Tool{{Name: "base_tool"}}, Messages: []goai.Message{{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "later"}}, ToolsAdded: []goai.Tool{{Name: "late_tool"}}}}}
	resolved := goai.ResolveTranscript(ctx, true)
	if len(resolved.Messages) != 2 || resolved.Messages[0].Role != goai.RoleSystem || resolved.Messages[1].Role != goai.RoleSystem {
		t.Fatalf("resolved messages=%#v", resolved.Messages)
	}
	if resolved.Messages[0].Content[0].Text != "legacy prompt" || resolved.Messages[1].Content[0].Text != "later" {
		t.Fatalf("system messages=%#v", resolved.Messages)
	}
	legacy := goai.ResolveContext(ctx, true)
	if legacy.SystemPrompt != "" || len(legacy.Messages) != 2 || legacy.Messages[1].Role != goai.RoleSystem || len(legacy.Tools) != 2 {
		t.Fatalf("native legacy context=%#v", legacy)
	}
}
