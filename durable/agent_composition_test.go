package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestAgentCompositionPinnedDeclarationReplacementSectionsAndRemovalWins(t *testing.T) {
	registry := NewRegistry()
	registration := func(name, description string) ToolRegistration {
		return ToolRegistration{Definition: goai.Tool{Name: name, Description: description, Parameters: json.RawMessage(`{"type":"object"}`)}, Implementation: "compose." + name, Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) { return ToolResult{}, nil }}
	}
	if err := registry.Register(registration("z-base", "base")); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(registration("a-base", "base")); err != nil {
		t.Fatal(err)
	}
	for _, extension := range []*Extension{{Name: "one", Tools: []ToolRegistration{registration("z-base", "one"), registration("x", "one")}, Sections: []PromptSection{{Key: "same", Render: func(context.Context, PromptInput) (*string, error) { return promptString("old"), nil }}, {Key: "other", Render: func(context.Context, PromptInput) (*string, error) { return promptString("other"), nil }}}}, {Name: "two", Tools: []ToolRegistration{registration("x", "two"), registration("y", "two")}, Sections: []PromptSection{{Key: "same", Render: func(context.Context, PromptInput) (*string, error) { return promptString("new"), nil }}}}} {
		if err := registry.Install(extension); err != nil {
			t.Fatal(err)
		}
	}
	offers, _, sections, _, err := registry.selectedGeneration(agentState{}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, offer := range offers {
		names = append(names, offer.Name)
	}
	if strings.Join(names, ",") != "z-base,a-base,x,y" || offers[2].Description != "two" {
		t.Fatal("composed insertion/replacement order", offers)
	}
	if len(sections) != 2 || sections[0].Key != "same" || sections[1].Key != "other" {
		t.Fatal("section overwrite position", sections)
	}
	text, err := sections[0].Render(bg, PromptInput{})
	if err != nil || *text != "new" {
		t.Fatal(text, err)
	}
	state := agentState{ExtensionFilter: &ExtensionFilter{Add: []string{"two"}, Remove: []string{"two"}}}
	offers, _, _, _, err = registry.selectedGeneration(state, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, offer := range offers {
		if offer.Name == "y" {
			t.Fatal("add overrode removal")
		}
	}
	registry.Remove("z-base")
	if err := registry.Register(registration("z-base", "again")); err != nil {
		t.Fatal(err)
	}
	offers, _, _, _, err = registry.selectedGeneration(agentState{}, DefaultLimits())
	if err != nil || offers[0].Name != "a-base" || offers[1].Name != "z-base" {
		t.Fatal("remove/readd order", offers, err)
	}
}
