package goai_test

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOpenRouter120CredentialCatalogRegionalLimitsPricesAndIsolation(t *testing.T) {
	baseline := []*goai.Model{{ID: "allowed", Provider: goai.ProviderOpenRouter, ContextWindow: 100, MaxTokens: 50, Cost: goai.ModelCost{Input: 9, Output: 9}}, {ID: "blocked", Provider: goai.ProviderOpenRouter}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/regional/models/user" || r.Header.Get("Authorization") != "Bearer key-one" {
			t.Error("regional/key boundary", r.URL, r.Header)
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"id": "allowed", "context_length": 200, "top_provider": map[string]any{"context_length": 300, "max_completion_tokens": 80}, "pricing": map[string]any{"prompt": "0.000001", "completion": "-1", "overrides": []any{map[string]any{"min_prompt_tokens": 500, "prompt": "0.000002"}, map[string]any{"min_prompt_tokens": 600, "utc_start": 1, "prompt": "0.000003"}, map[string]any{"min_prompt_tokens": 700, "utc_days": []string{}, "prompt": "0.000004"}, map[string]any{"min_prompt_tokens": 800, "utc_end": nil, "prompt": "0.000005"}}}}}})
	}))
	defer server.Close()
	models, e := goai.PersonalizeOpenRouterCatalog(context.Background(), server.Client(), server.URL+"/regional/", "key-one", baseline)
	if e != nil || len(models) != 1 {
		t.Fatal(models, e)
	}
	m := models[0]
	if m.ContextWindow != 300 || m.MaxTokens != 80 || m.Cost.Input != 1 || m.Cost.Output != 0 || len(m.Cost.Tiers) != 1 || m.Cost.Tiers[0].Input != 2 {
		t.Fatal(m)
	}
	m.ContextWindow = 999
	if baseline[0].ContextWindow != 100 || baseline[0].Cost.Input != 9 {
		t.Fatal("baseline mutation")
	}
}
func TestOpenRouter120FallbackMalformedEmptyAndCancelled(t *testing.T) {
	for _, mode := range []string{"401", "403", "404", "500", "malformed", "empty", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch mode {
				case "401":
					w.WriteHeader(401)
				case "403":
					w.WriteHeader(403)
				case "404":
					w.WriteHeader(404)
				case "500":
					w.WriteHeader(500)
				case "malformed":
					w.Write([]byte(`{"data":null}`))
				case "empty":
					w.Write([]byte(`{"data":[]}`))
				case "cancel":
					<-r.Context().Done()
				}
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if mode == "cancel" {
				cancel()
			}
			catalog := []*goai.Model{{ID: "test"}}
			models, e := goai.PersonalizeOpenRouterCatalog(ctx, server.Client(), server.URL, "key", catalog)
			switch mode {
			case "401", "403", "404":
				if e != nil || len(models) != 1 {
					t.Fatal(models, e)
				}
			case "empty":
				if e != nil || len(models) != 0 {
					t.Fatal(models, e)
				}
			default:
				if e == nil {
					t.Fatal("invalid response accepted", mode)
				}
			}
		})
	}
}
