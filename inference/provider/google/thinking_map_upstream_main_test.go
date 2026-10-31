package google

// Port of packages/ai/test/google-thinking-level-map.test.ts at
// 4b535d57ea8863dbba44b89872146523da271786. SDK config.thinkingConfig maps
// to REST generationConfig.thinkingConfig. Both registered adapters run their
// production Simple path; the payload hook stops before any network request.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func upstreamGoogleThinkingModel(api goai.Api, id string, mapping map[goai.ModelThinkingLevel]*string) *goai.Model {
	provider := goai.Provider("test-google")
	base := "https://example.invalid/v1beta"
	if api == goai.ApiGoogleVertex {
		provider = "test-vertex"
		base = "https://example.invalid/v1"
	}
	return &goai.Model{ID: id, Name: id, Api: api, Provider: provider, BaseURL: base, Reasoning: true, ThinkingLevelMap: mapping, Input: []string{"text"}, ContextWindow: 128000, MaxTokens: 4096}
}
func upstreamGoogleString(value string) *string { return &value }
func captureUpstreamGoogleThinking(t *testing.T, model *goai.Model, reasoning string, budgets *goai.ThinkingBudgets) map[string]any {
	t.Helper()
	var payload map[string]any
	sentinel := errors.New("payload captured")
	opts := &goai.StreamOptions{APIKey: "test", ThinkingBudgets: budgets, OnPayload: func(value any, _ *goai.Model) (any, error) {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &payload); err != nil {
			return nil, err
		}
		return nil, sentinel
	}}
	if reasoning != "" {
		level := goai.ThinkingLevel(reasoning)
		opts.Reasoning = &level
	}
	message := ""
	for event := range goai.GetApiProvider(model.Api).StreamSimple(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("Hello")}}, opts) {
		if e, ok := event.(*goai.ErrorEvent); ok {
			if e.Err != nil {
				message = e.Err.Error()
			}
			if e.Error != nil && e.Error.ErrorMessage != "" {
				message = e.Error.ErrorMessage
			}
		}
	}
	if !strings.Contains(message, "payload captured") {
		t.Fatalf("capture failed: %s", message)
	}
	config, ok := payload["generationConfig"].(map[string]any)
	if !ok {
		t.Fatal("generation config missing", payload)
	}
	thinking, ok := config["thinkingConfig"].(map[string]any)
	if !ok {
		t.Fatal("thinking config missing", config)
	}
	return thinking
}

func TestUpstreamMainGoogleThinkingMapResolver(t *testing.T) {
	for _, level := range []string{"minimal", "low", "medium", "high"} {
		model := upstreamGoogleThinkingModel(goai.ApiGoogleGenerativeAI, "gemini-3.7-flash", map[goai.ModelThinkingLevel]*string{})
		got, err := resolveGoogleThinkingLevel(model, goai.ModelThinkingLevel(level))
		if err != nil || got != level {
			t.Errorf("default %s: %s/%v", level, got, err)
		}
	}
	for _, mapped := range []string{"minimal", "low", "medium", "high", "MINIMAL", "LOW", "MEDIUM", "HIGH"} {
		model := upstreamGoogleThinkingModel(goai.ApiGoogleGenerativeAI, "gemini-3.7-flash", map[goai.ModelThinkingLevel]*string{"high": upstreamGoogleString(mapped), "xhigh": upstreamGoogleString(mapped), "max": upstreamGoogleString(mapped)})
		for _, level := range []string{"high", "xhigh", "max"} {
			got, err := resolveGoogleThinkingLevel(model, goai.ModelThinkingLevel(level))
			if err != nil || got != strings.ToLower(mapped) {
				t.Errorf("mapped %s/%s: %s/%v", level, mapped, got, err)
			}
		}
	}
	for _, tc := range []struct {
		mapping     map[goai.ModelThinkingLevel]*string
		level, want string
	}{{map[goai.ModelThinkingLevel]*string{"xhigh": upstreamGoogleString("extreme")}, "xhigh", "Unsupported Google thinking level mapping for test-google/gemini-3.7-flash: xhigh -> extreme"}, {map[goai.ModelThinkingLevel]*string{}, "max", "Unsupported Google thinking level mapping for test-google/gemini-3.7-flash: max -> undefined"}} {
		_, err := resolveGoogleThinkingLevel(upstreamGoogleThinkingModel(goai.ApiGoogleGenerativeAI, "gemini-3.7-flash", tc.mapping), goai.ModelThinkingLevel(tc.level))
		if err == nil || err.Error() != tc.want {
			t.Errorf("invalid %s: %v want %s", tc.level, err, tc.want)
		}
	}
}

func TestUpstreamMainGoogleThinkingMapAdapters(t *testing.T) {
	mapped := func() map[goai.ModelThinkingLevel]*string {
		return map[goai.ModelThinkingLevel]*string{"off": nil, "minimal": nil, "low": upstreamGoogleString("low"), "medium": upstreamGoogleString("medium"), "high": upstreamGoogleString("high"), "xhigh": nil, "max": nil}
	}
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, tc := range []struct {
			name, id, reasoning string
			mapping             map[goai.ModelThinkingLevel]*string
			want                map[string]any
		}{
			{"lowest when reasoning omitted", "gemini-3.8-flash", "", mapped(), map[string]any{"thinkingLevel": "LOW"}},
			{"preserves 3.1 Pro medium", "gemini-3.1-pro-preview", "medium", mapped(), map[string]any{"includeThoughts": true, "thinkingLevel": "MEDIUM"}},
			{"disables 2.5 when omitted", "gemini-2.5-flash", "", map[goai.ModelThinkingLevel]*string{}, map[string]any{"thinkingBudget": float64(0)}},
			{"Gemini 4 uses levels", "gemini-4-argon", "medium", map[goai.ModelThinkingLevel]*string{}, map[string]any{"includeThoughts": true, "thinkingLevel": "MEDIUM"}},
		} {
			t.Run(fmt.Sprint(api)+"/"+tc.name, func(t *testing.T) {
				got := captureUpstreamGoogleThinking(t, upstreamGoogleThinkingModel(api, tc.id, tc.mapping), tc.reasoning, nil)
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("thinking %#v want %#v", got, tc.want)
				}
			})
		}
	}
	for _, level := range []string{"xhigh", "max"} {
		t.Run("Google extended "+level, func(t *testing.T) {
			got := captureUpstreamGoogleThinking(t, upstreamGoogleThinkingModel(goai.ApiGoogleGenerativeAI, "gemini-3.7-flash", map[goai.ModelThinkingLevel]*string{"xhigh": upstreamGoogleString("high"), "max": upstreamGoogleString("high")}), level, nil)
			if got["includeThoughts"] != true || got["thinkingLevel"] != "HIGH" {
				t.Fatal(got)
			}
		})
	}
	t.Run("Google uppercase mapping", func(t *testing.T) {
		got := captureUpstreamGoogleThinking(t, upstreamGoogleThinkingModel(goai.ApiGoogleGenerativeAI, "gemini-3.7-flash", map[goai.ModelThinkingLevel]*string{"high": upstreamGoogleString("LOW")}), "high", nil)
		if got["thinkingLevel"] != "LOW" {
			t.Fatal(got)
		}
	})
	t.Run("Google mapped budget", func(t *testing.T) {
		budget := 1234
		got := captureUpstreamGoogleThinking(t, upstreamGoogleThinkingModel(goai.ApiGoogleGenerativeAI, "gemini-2.5-flash", map[goai.ModelThinkingLevel]*string{"xhigh": upstreamGoogleString("high")}), "xhigh", &goai.ThinkingBudgets{High: &budget})
		if got["thinkingBudget"] != float64(1234) {
			t.Fatal(got)
		}
	})
	t.Run("Vertex extended", func(t *testing.T) {
		got := captureUpstreamGoogleThinking(t, upstreamGoogleThinkingModel(goai.ApiGoogleVertex, "gemini-3.7-flash", map[goai.ModelThinkingLevel]*string{"xhigh": upstreamGoogleString("high")}), "xhigh", nil)
		if got["includeThoughts"] != true || got["thinkingLevel"] != "HIGH" {
			t.Fatal(got)
		}
	})
	t.Run("Vertex mapped budget", func(t *testing.T) {
		budget := 4321
		got := captureUpstreamGoogleThinking(t, upstreamGoogleThinkingModel(goai.ApiGoogleVertex, "gemini-2.5-flash", map[goai.ModelThinkingLevel]*string{"max": upstreamGoogleString("high")}), "max", &goai.ThinkingBudgets{High: &budget})
		if got["thinkingBudget"] != float64(4321) {
			t.Fatal(got)
		}
	})
}
