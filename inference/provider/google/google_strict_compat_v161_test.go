package google

import (
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV161GoogleStrictCompatibility(t *testing.T) {
	for _, adapter := range []struct {
		name     string
		api      goai.Api
		provider goai.Provider
	}{
		{"genai", goai.ApiGoogleGenerativeAI, goai.ProviderGoogle},
		{"vertex", goai.ApiGoogleVertex, goai.ProviderGoogleVertex},
	} {
		for _, tc := range []struct {
			name, modelID, compat, wantMode string
		}{
			{"prefixed-4", "randomprefix-gemini-4-argon", `{}`, "VALIDATED"},
			{"prefixed-2.5", "vendor-gemini-2.5-pro", `{}`, ""},
			{"alias-unset", "argon-latest", `{}`, ""},
			{"alias-enabled", "argon-latest", `{"googleCompat":{"supportsStrictMode":true}}`, "VALIDATED"},
			{"gemini-disabled", "gemini-3.1-pro-preview", `{"googleCompat":{"supportsStrictMode":false}}`, ""},
			{"gemini-empty-compat", "gemini-3.1-pro-preview", `{"googleCompat":{}}`, "VALIDATED"},
		} {
			t.Run(adapter.name+"/"+tc.name, func(t *testing.T) {
				model := makeGemini3UpstreamModel(adapter.api, adapter.provider, tc.modelID)
				model.BaseURL = "https://example.invalid/v1"
				if err := json.Unmarshal([]byte(tc.compat), model); err != nil {
					t.Fatal(err)
				}
				tool := goai.Tool{Name: "test_tool", Parameters: json.RawMessage(`{"type":"object","properties":{}}`),
					ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"}}
				transcript := &goai.Context{Messages: []goai.Message{goai.UserMessage("Hi")}, Tools: []goai.Tool{tool}}
				captured := errors.New("payload captured")
				onPayloadCount, attemptCount := 0, 0
				var mode string
				hook := func(payload any, _ *goai.Model) (any, error) {
					onPayloadCount++
					data, err := json.Marshal(payload)
					if err != nil {
						return nil, err
					}
					var wire struct {
						ToolConfig *geminiToolConfig `json:"toolConfig"`
					}
					if err := json.Unmarshal(data, &wire); err != nil {
						return nil, err
					}
					if wire.ToolConfig != nil && wire.ToolConfig.FunctionCallingConfig != nil {
						mode = wire.ToolConfig.FunctionCallingConfig.Mode
					}
					return nil, captured
				}
				transport := vertexTokenWireFunc(func(r *http.Request) (*http.Response, error) {
					defer r.Body.Close()
					attemptCount++
					return nil, errors.New("unexpected HTTP attempt after the capture hook")
				})
				var terminal error
				for event := range goai.GetApiProvider(adapter.api).StreamSimple(t.Context(), model, transcript,
					&goai.StreamOptions{APIKey: "test", OnPayload: hook, HTTPTransport: transport}) {
					if err, ok := event.(*goai.ErrorEvent); ok {
						terminal = err.Err
					}
				}
				if !errors.Is(terminal, captured) || onPayloadCount != 1 || attemptCount != 0 {
					t.Fatalf("expected one capture and no HTTP attempt, got %v, %d calls, %d attempts", terminal, onPayloadCount, attemptCount)
				}
				if mode != tc.wantMode {
					t.Errorf("function calling mode = %q; want %q", mode, tc.wantMode)
				}
			})
		}
	}
}
