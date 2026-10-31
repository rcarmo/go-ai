package google

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func googleStrictToolMain(mode string) goai.Tool {
	return goai.Tool{Name: "tool", Parameters: json.RawMessage(`{"type":"object","properties":{"x":{"type":"string"}},"required":["x"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: mode}}
}
func TestGoogleStrictModeMatrixMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		for _, modelID := range []string{"gemini-2.5-pro", "gemini-3.1-pro-preview"} {
			for _, sampling := range []string{"plain", "prefer", "require"} {
				for _, choice := range []goai.ToolChoice{"", goai.ToolChoiceAuto, goai.ToolChoiceNone, goai.ToolChoiceRequired, "unknown"} {
					t.Run(fmt.Sprint(api, "/", modelID, "/", sampling, "/", choice), func(t *testing.T) {
						tool := googleStrictToolMain(sampling)
						if sampling == "plain" {
							tool.ConstrainedSampling = nil
						}
						req, err := buildRequest(&goai.Model{ID: modelID, Api: api}, &goai.Context{Tools: []goai.Tool{tool}}, &goai.StreamOptions{ToolChoice: choice})
						if modelID == "gemini-2.5-pro" && sampling == "require" {
							if err == nil {
								t.Fatal("unsupported requirement accepted")
							}
							return
						}
						if err != nil {
							t.Fatal(err)
						}
						want := ""
						if choice != "" {
							want = "AUTO"
						}
						if modelID == "gemini-3.1-pro-preview" && sampling != "plain" {
							want = "VALIDATED"
						}
						switch choice {
						case goai.ToolChoiceNone:
							want = "NONE"
						case goai.ToolChoiceRequired:
							want = "ANY"
						}
						if want == "" {
							if req.ToolConfig != nil {
								t.Fatal("unexpected mode", req.ToolConfig)
							}
							return
						}
						if req.ToolConfig == nil || req.ToolConfig.FunctionCallingConfig.Mode != want {
							t.Fatal("wrong function mode", req.ToolConfig, want)
						}
						if sampling != "plain" && modelID == "gemini-3.1-pro-preview" {
							var schema map[string]any
							if err := json.Unmarshal(req.Tools[0].FunctionDeclarations[0].ParametersJsonSchema, &schema); err != nil {
								t.Fatal(err)
							}
							if schema["additionalProperties"] != false {
								t.Fatal("strict schema not converted", schema)
							}
						}
					})
				}
			}
		}
	}
}
func TestGoogleStrictAllRequirementsMain(t *testing.T) {
	invalid := googleStrictToolMain("require")
	invalid.Name = "invalid"
	invalid.Parameters = json.RawMessage(`{"type":"array","items":{"type":"string"}}`)
	for _, choice := range []goai.ToolChoice{"", goai.ToolChoiceNone, goai.ToolChoiceRequired} {
		_, err := buildRequest(&goai.Model{ID: "gemini-3.1-pro"}, &goai.Context{Tools: []goai.Tool{googleStrictToolMain("prefer"), invalid}}, &goai.StreamOptions{ToolChoice: choice})
		if err == nil {
			t.Fatal("later requirement bypassed", choice)
		}
	}
	invalid.ConstrainedSampling.Strict = "prefer"
	req, err := buildRequest(&goai.Model{ID: "gemini-3.1-pro"}, &goai.Context{Tools: []goai.Tool{invalid}}, nil)
	if err != nil || req.ToolConfig != nil {
		t.Fatal("preferred unsupported schema did not fall back", req.ToolConfig, err)
	}
	invalid.Parameters = json.RawMessage(`{"type":`)
	if _, err := buildRequest(&goai.Model{ID: "gemini-3.1-pro"}, &goai.Context{Tools: []goai.Tool{invalid}}, nil); err == nil {
		t.Fatal("malformed preferred schema suppressed")
	}
	for _, choice := range []goai.ToolChoice{"", goai.ToolChoiceAuto, goai.ToolChoiceNone, goai.ToolChoiceRequired} {
		req, err := buildRequest(&goai.Model{ID: "gemini-3.1-pro"}, &goai.Context{}, &goai.StreamOptions{ToolChoice: choice})
		if err != nil || req.ToolConfig != nil {
			t.Fatal("tool-less request configured calling", choice, err)
		}
	}
}
func TestGoogleStrictFailureBeforeHooksAndHTTPMain(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }))
	defer server.Close()
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		payloadCalls := 0
		opts := &goai.StreamOptions{APIKey: "test-key", OnPayload: func(payload any, model *goai.Model) (any, error) { payloadCalls++; return payload, nil }}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		var terminal *goai.ErrorEvent
		for event := range streamGoogle(ctx, &goai.Model{ID: "gemini-2.5-pro", Api: api, Provider: goai.ProviderGoogle, BaseURL: server.URL}, &goai.Context{Tools: []goai.Tool{googleStrictToolMain("require")}}, opts) {
			if e, ok := event.(*goai.ErrorEvent); ok {
				if terminal != nil {
					t.Fatal("duplicate failure")
				}
				terminal = e
			} else {
				t.Fatal("unexpected preflight event", event)
			}
		}
		cancel()
		if terminal == nil || terminal.Err == nil || terminal.Reason != goai.StopReasonError || !strings.Contains(terminal.Err.Error(), "requires JSON-schema constrained sampling") {
			t.Fatal("missing strict failure", terminal)
		}
		if payloadCalls != 0 || calls.Load() != 0 {
			t.Fatal("unsupported strict request reached hooks/network", payloadCalls, calls.Load())
		}
	}
}

func TestGoogleValidatedModeWireMain(t *testing.T) {
	for _, api := range []goai.Api{goai.ApiGoogleGenerativeAI, goai.ApiGoogleVertex} {
		t.Run(string(api), func(t *testing.T) {
			var calls atomic.Int32
			var invalid atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body geminiRequest
				if json.NewDecoder(r.Body).Decode(&body) != nil || body.ToolConfig == nil || body.ToolConfig.FunctionCallingConfig == nil || body.ToolConfig.FunctionCallingConfig.Mode != "VALIDATED" {
					invalid.Store(true)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n")
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			done := false
			for event := range streamGoogle(ctx, &goai.Model{ID: "gemini-3.1-pro", Api: api, Provider: goai.ProviderGoogle, BaseURL: server.URL}, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}, Tools: []goai.Tool{googleStrictToolMain("require")}}, &goai.StreamOptions{APIKey: "test-key", Project: "test-project", Location: "test-location"}) {
				switch e := event.(type) {
				case *goai.ErrorEvent:
					t.Fatal(e.Err)
				case *goai.DoneEvent:
					done = true
				}
			}
			if calls.Load() != 1 || invalid.Load() || !done {
				t.Fatal("VALIDATED mode missing from wire", calls.Load(), invalid.Load(), done)
			}
		})
	}
}
