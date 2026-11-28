package openai

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Ported from the unchanged official pi v1.7.0 originals:
// packages/ai/test/baseten-models.test.ts at fb0fe823539f6d04850f6e34748c01f01f212e1f.
func TestV170BasetenModelsUnchangedOriginal(t *testing.T) {
	goai.RegisterBuiltinModels()
	models := []string{"zai-org/GLM-5.2", "zai-org/GLM-5.2-Fast"}
	for _, id := range models {
		model := goai.GetModel(goai.ProviderBaseten, id)
		if model == nil {
			t.Fatalf("missing Baseten model %q", id)
		}
		if !reflect.DeepEqual(model.Input, []string{"text"}) {
			t.Errorf("%s input = %v, want [text]", id, model.Input)
		}
	}

	model := goai.GetModel(goai.ProviderBaseten, "zai-org/GLM-5.2")
	for _, tc := range []struct {
		name      string
		reasoning *goai.ThinkingLevel
		enabled   bool
		effort    string
	}{
		{name: "thinking on", reasoning: ptrThinkingLevel(goai.ThinkingHigh), enabled: true, effort: "high"},
		{name: "thinking off", enabled: false, effort: "none"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			captured := errors.New("payload captured")
			_, err := goai.Complete(t.Context(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("test")}}, &goai.StreamOptions{
				APIKey:    "test-baseten-key",
				Reasoning: tc.reasoning,
				OnPayload: func(value any, _ *goai.Model) (any, error) {
					encoded, marshalErr := json.Marshal(value)
					if marshalErr != nil {
						return nil, marshalErr
					}
					if err := json.Unmarshal(encoded, &payload); err != nil {
						return nil, err
					}
					return nil, captured
				},
			})
			if !errors.Is(err, captured) {
				t.Fatalf("capture error = %v", err)
			}
			args, ok := payload["chat_template_args"].(map[string]any)
			if !ok || args["enable_thinking"] != tc.enabled {
				t.Fatalf("chat_template_args = %#v, want enable_thinking=%v", payload["chat_template_args"], tc.enabled)
			}
			if payload["reasoning_effort"] != tc.effort {
				t.Fatalf("reasoning_effort = %#v, want %q", payload["reasoning_effort"], tc.effort)
			}
		})
	}

	t.Setenv("BASETEN_API_KEY", "test-baseten-key")
	if got := goai.FindEnvKeys(goai.ProviderBaseten, nil); !reflect.DeepEqual(got, []string{"BASETEN_API_KEY"}) {
		t.Fatalf("FindEnvKeys = %v", got)
	}
	if got := goai.GetEnvAPIKey(goai.ProviderBaseten); got != "test-baseten-key" {
		t.Fatal("BASETEN_API_KEY was not resolved")
	}
}

func ptrThinkingLevel(value goai.ThinkingLevel) *goai.ThinkingLevel { return &value }
