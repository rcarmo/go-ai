package durable

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTool120GrammarConstraintDurableHTTPAndIntent(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		observed := make(chan map[string]any, 1)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
			observed <- body
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprint(w, "data: {\"id\":\"x\",\"choices\":[{\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}))
		defer server.Close()
		registry := NewRegistry()
		reg := ToolRegistration{Definition: goai.Tool{Name: "grammar_wire", Parameters: json.RawMessage(`{"type":"object","properties":{"code":{"type":"string"}}}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": "start: /.+/"}}}, Implementation: "wire", Version: 1, Execute: func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			t.Error("unexpected execution")
			return ToolResult{}, nil
		}}
		if err := registry.Register(reg); err != nil {
			t.Fatal(err)
		}
		yes := true
		model := fakeModel(goai.ApiOpenAICompletions)
		model.BaseURL = server.URL + "/v1"
		model.CompletionsCompat = &goai.OpenAICompletionsCompat{SupportsOpenAIGrammarTools: &yes}
		h := openHarness(t, b.store, Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, ModelRef) (*goai.StreamOptions, error) {
			return &goai.StreamOptions{APIKey: "fixture"}, nil
		}})
		conv := root(t, h, ModelRef{Provider: model.Provider, ID: model.ID})
		sub, err := conv.Submit(bg, Input{Content: "hello"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		if _, err = sub.Wait(ctx); err != nil {
			t.Fatal(err)
		}
		body := watchReceive(t, observed)
		tools := body["tools"].([]any)
		tool := tools[0].(map[string]any)
		format, ok := tool["format"].(map[string]any)
		if !ok {
			t.Fatal("grammar format missing", body)
		}
		if tool["type"] != "custom" || tool["name"] != "grammar_wire" || format["definition"] != "start: /.+/" {
			t.Fatal(body)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, task := range state.Tasks {
			if task.Kind == "pi.generation" {
				var cp generationCheckpoint
				if err = fromObject(task.Checkpoint, &cp, h.session.limits); err != nil {
					t.Fatal(err)
				}
				if len(cp.Offered) == 1 && cp.Offered[0].ConstrainedSampling != nil {
					found = true
					if cp.Offered[0].ConstrainedSampling.Variants["openai_lark"] != "start: /.+/" {
						t.Fatal(cp)
					}
				}
			}
		}
		if !found {
			t.Fatal("constraint missing from durable intent")
		}
	})
}
