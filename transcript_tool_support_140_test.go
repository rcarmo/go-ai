package goai

import (
	"testing"
)

// Mirrors upstream packages/ai/test/transcript-tool-changes.test.ts
// ("resolves the top-level tool boundary per transport", #10940).
func TestTranscript140ToolBoundaryPerSupport(t *testing.T) {
	base := Tool{Name: "base_tool"}
	drop := Tool{Name: "drop_tool"}
	lateX := Tool{Name: "late_tool", Description: "x"}
	lateY := Tool{Name: "late_tool", Description: "y"}

	messages := []Message{
		{Role: RoleSystem, ToolsAdded: []Tool{base, drop}, Timestamp: 0},
		{Role: RoleSystem, ToolsRemoved: []ToolReference{{Name: "drop_tool"}}, Timestamp: 1},
		{Role: RoleSystem, ToolsAdded: []Tool{drop}, Timestamp: 2},
		{Role: RoleSystem, ToolsAdded: []Tool{lateX}, Timestamp: 3},
		{Role: RoleSystem, ToolsAdded: []Tool{lateY}, Timestamp: 4},
	}

	// Before any non-additive change: the anchor stays the boundary.
	got := ResolveTranscriptTools(messages[:2], ToolSupportAdditions)
	if got.FoldedSystemMessages != 2 || len(got.RequestTools) != 1 || got.RequestTools[0].Name != "base_tool" {
		t.Fatalf("additions slice(0,2): %+v", got)
	}

	// Kimi K3 ignores a removed tool that a later message declares again, so the
	// re-added drop_tool moves the boundary; the later addition stays anchored.
	got = ResolveTranscriptTools(messages[:4], ToolSupportAdditions)
	if got.FoldedSystemMessages != 3 || len(got.RequestTools) != 2 {
		t.Fatalf("additions slice(0,4): %+v", got)
	}
	for i, want := range []string{"base_tool", "drop_tool"} {
		if got.RequestTools[i].Name != want {
			t.Fatalf("additions slice(0,4) order: %v", got.RequestTools)
		}
	}

	// The redefinition of late_tool moves the boundary to its message.
	got = ResolveTranscriptTools(messages, ToolSupportAdditions)
	if got.FoldedSystemMessages != 5 || len(got.RequestTools) != 3 {
		t.Fatalf("additions full: %+v", got)
	}
	if got.RequestTools[2].Name != "late_tool" || got.RequestTools[2].Description != "y" {
		t.Fatalf("additions full redefinition: %v", got.RequestTools)
	}

	got = ResolveTranscriptTools(messages, ToolSupportChanges)
	if got.FoldedSystemMessages != 0 || len(got.RequestTools) != 0 {
		t.Fatalf("changes: %+v", got)
	}

	got = ResolveTranscriptTools(messages, ToolSupportNone)
	if got.FoldedSystemMessages != 5 || len(got.RequestTools) != 3 || got.RequestTools[2].Description != "y" {
		t.Fatalf("none: %+v", got)
	}
}

func TestTranscript140HasNonAdditiveToolChanges(t *testing.T) {
	if !HasNonAdditiveToolChanges([]Message{
		{Role: RoleSystem, ToolsAdded: []Tool{{Name: "a"}}},
		{Role: RoleSystem, ToolsRemoved: []ToolReference{{Name: "a"}}},
	}) {
		t.Fatal("removal must count as non-additive")
	}
	if HasNonAdditiveToolChanges([]Message{
		{Role: RoleSystem, ToolsAdded: []Tool{{Name: "a"}}},
		{Role: RoleSystem, ToolsAdded: []Tool{{Name: "b"}}},
	}) {
		t.Fatal("plain additions must stay additive")
	}
	if !HasNonAdditiveToolChanges([]Message{
		{Role: RoleSystem, ToolsAdded: []Tool{{Name: "a"}}},
		{Role: RoleSystem, ToolsAdded: []Tool{{Name: "a", Description: "changed"}}},
	}) {
		t.Fatal("same-name redeclaration must count as non-additive")
	}

	// A name declared before counts as a redeclaration even after removal (Kimi K3).
	if !HasNonAdditiveToolChanges([]Message{
		{Role: RoleSystem, ToolsAdded: []Tool{{Name: "a"}}},
		{Role: RoleSystem, ToolsRemoved: []ToolReference{{Name: "a"}}},
		{Role: RoleSystem, ToolsAdded: []Tool{{Name: "a"}}},
	}) {
		t.Fatal("redeclaration after removal must count as non-additive")
	}

	// Non-system messages are ignored.
	if HasNonAdditiveToolChanges([]Message{
		{Role: RoleUser, Content: []ContentBlock{{Type: "text", Text: "hi"}}},
		{Role: RoleSystem, ToolsAdded: []Tool{{Name: "a"}}},
	}) {
		t.Fatal("no tool change here")
	}
}

func TestTranscript140FoldedAnchorKeepsAnchorAndCurrentTools(t *testing.T) {
	messages := []Message{
		{Role: RoleSystem, Content: []ContentBlock{{Type: "text", Text: "Initial tools A,B."}}, ToolsAdded: []Tool{{Name: "A"}, {Name: "B"}}, Timestamp: 1},
		{Role: RoleSystem, ToolsRemoved: []ToolReference{{Name: "B"}}, Timestamp: 2},
		{Role: RoleSystem, ToolsAdded: []Tool{{Name: "B"}}, Timestamp: 3},
	}

	// Native support keeps the anchor and every later change verbatim.
	native := ResolveTranscript(&Context{Messages: messages}, true)
	if len(native.Messages) != 3 || ContentText(native.Messages[0].Content) != "Initial tools A,B." {
		t.Fatalf("native: %+v", native)
	}
	if len(native.Messages[1].ToolsRemoved) != 1 || len(native.Messages[2].ToolsAdded) != 1 {
		t.Fatalf("native must keep later changes: %+v", native.Messages)
	}

	// Without mid-conversation support the anchor text survives and the current
	// tool state is folded into a single declaration.
	folded := ResolveTranscript(&Context{Messages: messages}, false)
	if len(folded.Messages) != 1 {
		t.Fatalf("folded: %+v", folded)
	}
	text := ContentText(folded.Messages[0].Content)
	if text != "Initial tools A,B." {
		t.Fatalf("anchor text must survive folding: %q", text)
	}
	tools := folded.Messages[0].ToolsAdded
	if len(tools) != 2 || tools[0].Name != "A" || tools[1].Name != "B" {
		t.Fatalf("current tools must fold in order: %v", tools)
	}

	// No anchor text: only the current declaration remains.
	only := ResolveTranscript(&Context{Messages: []Message{{Role: RoleSystem, ToolsAdded: []Tool{{Name: "A"}, {Name: "B"}}}}}, false)
	if len(only.Messages) != 1 || ContentText(only.Messages[0].Content) != "" {
		t.Fatalf("no anchor text: %+v", only)
	}

	// Empty system with no tools renders nothing.
	empty := ResolveTranscript(&Context{Messages: []Message{{Role: RoleUser, Content: []ContentBlock{{Type: "text", Text: "hello"}}}}}, false)
	if len(empty.Messages) != 1 || empty.Messages[0].Role != RoleUser {
		t.Fatalf("empty system: %+v", empty)
	}
}

func TestTranscript140NormalizeDoesNotMutateContext(t *testing.T) {
	ctx := &Context{SystemPrompt: "Initial tools A,B.", Tools: []Tool{{Name: "A"}, {Name: "B"}}}
	got := NormalizeContext(ctx)
	if len(got.Messages) != 1 || got.Messages[0].Role != RoleSystem {
		t.Fatalf("normalize: %+v", got)
	}
	if ContentText(got.Messages[0].Content) != "Initial tools A,B." || len(got.Messages[0].ToolsAdded) != 2 {
		t.Fatalf("normalize content: %+v", got.Messages[0])
	}
	if ctx.SystemPrompt != "Initial tools A,B." || len(ctx.Tools) != 2 {
		t.Fatalf("context was mutated: %+v", ctx)
	}

	// Round-trip back to the legacy shape.
	back := ContextFromTranscript(got)
	if back.SystemPrompt != "Initial tools A,B." || len(back.Messages) != 0 || len(back.Tools) != 2 {
		t.Fatalf("round-trip: %+v", back)
	}
}
