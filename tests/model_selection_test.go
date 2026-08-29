package goai_test

import (
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestParseModelRefRoundTrip(t *testing.T) {
	ref, err := goai.ParseModelRef("github-copilot/gpt-5.4")
	if err != nil {
		t.Fatal(err)
	}
	if ref.Provider != goai.ProviderGitHubCopilot || ref.ID != "gpt-5.4" {
		t.Fatalf("unexpected ref: %#v", ref)
	}
	if got := ref.String(); got != "github-copilot/gpt-5.4" {
		t.Fatalf("String() = %q", got)
	}
}

func TestParseModelRefRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"", "gpt-5.4", "github-copilot/", "/gpt-5.4"} {
		if _, err := goai.ParseModelRef(value); err == nil {
			t.Fatalf("expected error for %q", value)
		}
	}
}

func TestModelPickerItemsSortsAndFiltersForUI(t *testing.T) {
	models := []*goai.Model{
		{ID: "z", Name: "Zulu", Provider: goai.ProviderOpenAI},
		{ID: "b", Name: "Beta", Provider: goai.ProviderGitHubCopilot},
		{ID: "a", Name: "Alpha", Provider: goai.ProviderGitHubCopilot},
		nil,
	}
	got := goai.ModelPickerItems(models, goai.ProviderGitHubCopilot)
	want := []string{"github-copilot/a", "github-copilot/b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("picker items = %#v, want %#v", got, want)
	}
}

func TestFindModelByRefUsesRuntimeModelList(t *testing.T) {
	models := []*goai.Model{{ID: "gpt-5.4", Provider: goai.ProviderGitHubCopilot}}
	model := goai.FindModelByRef(models, goai.ModelRef{Provider: goai.ProviderGitHubCopilot, ID: "gpt-5.4"})
	if model == nil || model.ID != "gpt-5.4" {
		t.Fatalf("unexpected model: %#v", model)
	}
	if missing := goai.FindModelByRef(models, goai.ModelRef{Provider: goai.ProviderGitHubCopilot, ID: "missing"}); missing != nil {
		t.Fatalf("unexpected missing match: %#v", missing)
	}
}

func TestSwitchModelTransformsProviderSpecificTranscriptState(t *testing.T) {
	ctx := &goai.Context{Messages: []goai.Message{
		{
			Role:     goai.RoleAssistant,
			Provider: goai.ProviderGitHubCopilot,
			Api:      goai.ApiAnthropicMessages,
			Model:    "claude-sonnet-4.6",
			Content: []goai.ContentBlock{
				{Type: "thinking", Thinking: "secret", ThinkingSignature: "sig"},
				{Type: "text", Text: "hello"},
			},
		},
	}}
	newModel := &goai.Model{ID: "gpt-5.4", Provider: goai.ProviderGitHubCopilot, Api: goai.ApiOpenAIResponses, Input: []string{"text"}}
	switched := goai.SwitchModel(ctx, newModel)
	if switched == ctx {
		t.Fatal("SwitchModel should return a copied context")
	}
	if len(switched.Messages) != 1 || len(switched.Messages[0].Content) != 2 {
		t.Fatalf("unexpected transformed messages: %#v", switched.Messages)
	}
	if switched.Messages[0].Content[0].Type != "text" || switched.Messages[0].Content[0].ThinkingSignature != "" {
		t.Fatalf("thinking block was not normalized to unsigned text: %#v", switched.Messages[0].Content[0])
	}
	if len(ctx.Messages[0].Content) != 2 {
		t.Fatalf("original context was mutated: %#v", ctx.Messages)
	}
}
