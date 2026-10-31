package openaicodex

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func codexStrictTool(schema, mode string) goai.Tool {
	return goai.Tool{Name: "strict_tool", Parameters: json.RawMessage(schema), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: mode}}
}

func TestCodexStrictDeclarationSamplingMain(t *testing.T) {
	valid := `{"type":"object","properties":{"x":{"type":"string"}}}`
	unsupported := `{"type":"object","oneOf":[]}`
	for _, supported := range []bool{false, true} {
		for _, mode := range []string{"prefer", "require"} {
			for _, schema := range []string{valid, unsupported, "{"} {
				t.Run(fmt.Sprintf("support_%t/%s/%s", supported, mode, schema), func(t *testing.T) {
					model := codexGrammarModel()
					model.ResponsesCompat.SupportsStrictMode = &supported
					tool := codexStrictTool(schema, mode)
					before, _ := json.Marshal(tool)
					hooks := 0
					opts := &goai.StreamOptions{OnPayload: func(payload any, _ *goai.Model) (any, error) { hooks++; return payload, nil }}
					prepared, err := prepareCodexRequest(context.Background(), model, &goai.Context{Tools: []goai.Tool{tool}, Messages: []goai.Message{goai.UserMessage("hello")}}, opts)
					wantError := (mode == "require" && (!supported || schema != valid)) || schema == "{"
					if wantError {
						wantHooks := 0
						if !supported && mode == "prefer" && schema == "{" {
							wantHooks = 1
						} // Existing non-strict payload-hook repair remains available.
						if err == nil || hooks != wantHooks {
							t.Fatal("sampling failure hook boundary", err, hooks, wantHooks)
						}
						return
					}
					if err != nil || hooks != 1 {
						t.Fatal("valid/fallback sampling rejected", err, hooks)
					}
					var body struct {
						Tools []struct {
							Strict     *bool          `json:"strict"`
							Parameters map[string]any `json:"parameters"`
						} `json:"tools"`
					}
					if err = json.Unmarshal(prepared.bodyJSON, &body); err != nil {
						t.Fatal(err)
					}
					if len(body.Tools) != 1 {
						t.Fatal("lost declaration", string(prepared.bodyJSON))
					}
					definition := body.Tools[0]
					if !supported {
						if definition.Strict != nil {
							t.Fatal("unsupported strict flag emitted", definition)
						}
					} else {
						wantStrict := schema == valid
						if definition.Strict == nil || *definition.Strict != wantStrict {
							t.Fatal("wrong sampling mode", definition)
						}
						if wantStrict {
							if definition.Parameters["additionalProperties"] != false {
								t.Fatal("strict schema not converted", definition)
							}
						}
					}
					after, _ := json.Marshal(tool)
					if string(before) != string(after) {
						t.Fatal("sampling mutated caller schema")
					}
				})
			}
		}
	}
}

func TestCodexRequiredSamplingRejectsBeforeTransportMain(t *testing.T) {
	for _, transport := range []goai.Transport{goai.TransportSSE, goai.TransportWebSocket, goai.TransportAuto} {
		t.Run(string(transport), func(t *testing.T) {
			requests := make(chan struct{}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests <- struct{}{}; w.WriteHeader(500) }))
			defer server.Close()
			model := codexGrammarModel()
			model.BaseURL = server.URL
			tool := codexStrictTool(`{"type":"object","oneOf":[]}`, "require")
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			failures, hooks := 0, 0
			for event := range streamCodex(ctx, model, &goai.Context{Tools: []goai.Tool{tool}}, &goai.StreamOptions{APIKey: "test", Transport: transport, OnPayload: func(payload any, _ *goai.Model) (any, error) { hooks++; return payload, nil }}) {
				if e, ok := event.(*goai.ErrorEvent); ok {
					failures++
					if e.Err == nil || !strings.Contains(e.Err.Error(), "requires JSON-schema") {
						t.Error("unexpected failure", e.Err)
					}
				}
			}
			if failures != 1 || hooks != 0 {
				t.Fatal("required sampling reached hook", failures, hooks)
			}
			select {
			case <-requests:
				t.Fatal("required sampling reached transport")
			default:
			}
		})
	}
}

func TestCodexStrictSamplingProjectionMain(t *testing.T) {
	enabled := true
	valid := codexStrictTool(`{"type":"object"}`, "require")
	invalid := codexStrictTool(`{"type":"object","oneOf":[]}`, "require")
	input := &goai.Context{Messages: []goai.Message{{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{invalid}}, {Role: goai.RoleSystem, ToolsAdded: []goai.Tool{valid}}}}
	model := codexGrammarModel()
	// A collapsed snapshot validates only the final declaration.
	if _, err := prepareCodexRequest(context.Background(), model, input, nil); err != nil {
		t.Fatal("folded historical schema rejected", err)
	}
	model.ResponsesCompat.SupportsMidConvoSystemMessages = &enabled
	model.ResponsesCompat.SupportsAdditionalTools = &enabled
	if _, err := prepareCodexRequest(context.Background(), model, input, nil); err == nil {
		t.Fatal("anchored invalid historical schema accepted")
	}
	model.ResponsesCompat.SupportsAdditionalTools = nil
	model.ResponsesCompat.SupportsToolSearch = &enabled
	if _, err := prepareCodexRequest(context.Background(), model, input, nil); err != nil {
		t.Fatal("search-folded old declaration rejected", err)
	}
	input.Messages = append(input.Messages, goai.Message{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{func() goai.Tool { tool := invalid; tool.Name = "later"; return tool }()}})
	if _, err := prepareCodexRequest(context.Background(), model, input, nil); err == nil {
		t.Fatal("invalid unfolded search declaration accepted")
	}
}
