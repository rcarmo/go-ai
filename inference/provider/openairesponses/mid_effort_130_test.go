package openairesponses

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestResponses130StreamRetainsEffectiveEffort(t *testing.T) {
	model := &goai.Model{Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, ID: "gpt-6"}
	var message *goai.Message
	processStreamWithOptions(strings.NewReader("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n"), model, &goai.StreamOptions{}, func(event goai.Event) {
		if done, ok := event.(*goai.DoneEvent); ok {
			message = done.Message
		}
	}, responsesToolView{providerThinkingLevel: "high"})
	if message == nil || message.ProviderThinkingLevel != "high" {
		t.Fatal(message)
	}
}

func TestResponses130ConfigurationEffortReplayAndGates(t *testing.T) {
	yes := true
	level := goai.ThinkingLevel("high")
	model := &goai.Model{Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, ID: "gpt-6", BaseURL: "https://api.openai.com/v1", Reasoning: true, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsMidConvoEffort: &yes}}
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi"), {Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: model.ID, ProviderThinkingLevel: "low", Content: []goai.ContentBlock{{Type: "text", Text: "earlier"}}}, {Role: goai.RoleAssistant, Provider: model.Provider, Api: model.Api, Model: "other", ProviderThinkingLevel: "medium", Content: []goai.ContentBlock{{Type: "text", Text: "foreign"}}}}}
	for _, tc := range []struct {
		name, key          string
		enabled, reasoning bool
		want               []string
	}{{"enabled", "sk-test", true, true, []string{"low", "high"}}, {"off-gate", "sk-test", false, true, nil}, {"no-option", "sk-test", true, false, nil}, {"sign-in", "oauth-token", true, true, nil}} {
		t.Run(tc.name, func(t *testing.T) {
			enabled := tc.enabled
			model.ResponsesCompat.SupportsMidConvoEffort = &enabled
			opts := &goai.StreamOptions{APIKey: tc.key}
			if tc.reasoning {
				opts.Reasoning = &level
			}
			req := buildRequest(model, ctx, opts)
			var items []map[string]interface{}
			if err := json.Unmarshal(req.Input, &items); err != nil {
				t.Fatal(err)
			}
			var efforts []string
			for _, item := range items {
				if item["type"] == "configuration_update" {
					efforts = append(efforts, item["reasoning"].(map[string]interface{})["effort"].(string))
				}
			}
			if !reflect.DeepEqual(efforts, tc.want) {
				t.Fatal(efforts, tc.want)
			}
			if len(efforts) > 0 && (req.Reasoning == nil || req.Reasoning.Effort != "medium") {
				t.Fatal("request effort not pinned", req.Reasoning)
			}
		})
	}
}
