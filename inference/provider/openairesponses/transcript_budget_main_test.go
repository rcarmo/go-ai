package openairesponses

import (
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestResponsesTranscriptSectionBudgetAndNilOptionsMain(t *testing.T) {
	enabled, disabled := true, false
	model := responsesDeferredModel("gpt-5.4", goai.ProviderOpenAI)
	model.ContextWindow = 50000
	model.Reasoning = true
	model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsMidConvoSystemMessages: &enabled, SupportsAdditionalTools: &disabled, SupportsToolSearch: &disabled}
	value := "section budget material"
	conv := &goai.Context{Messages: []goai.Message{{Role: goai.RoleSystem, Sections: map[string]*string{"scope": &value}}, goai.UserMessage("hello")}}
	maximum := 40000
	opts := &goai.StreamOptions{MaxTokens: &maximum}
	request := buildRequest(model, conv, opts)
	expected := goai.ClampStreamMaxTokensPtr(model, &goai.Context{Messages: []goai.Message{goai.UserMessage(string(request.Input))}}, opts)
	if request.MaxOutputTokens == nil || expected == nil || *request.MaxOutputTokens != *expected {
		t.Fatal("section content missing from wire budget", request.MaxOutputTokens, expected)
	}
	if request.MaxOutputTokens = buildRequest(model, conv, nil).MaxOutputTokens; request.MaxOutputTokens != nil {
		t.Fatal("nil options unexpectedly enabled max-output parameter")
	}
	model.ResponsesCompat.SupportsAdditionalTools = &enabled
	if request = buildRequest(model, conv, nil); request.MaxOutputTokens != nil {
		t.Fatal("nil options enabled max-output for changes mode")
	}
}
