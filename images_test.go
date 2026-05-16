package goai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
	_ "github.com/rcarmo/go-ai/provider/openai"
)

func TestImageAPIProviderRegistered(t *testing.T) {
	p := goai.GetImagesApiProvider(goai.ImagesApiOpenRouter)
	if p == nil {
		t.Fatal("expected openrouter images provider to be registered")
	}
}

func TestBuiltinImageModels(t *testing.T) {
	goai.RegisterBuiltinImageModels()
	providers := goai.ListImageProviders()
	if len(providers) != 1 || providers[0] != goai.ImagesProviderOpenRouter {
		t.Fatalf("expected openrouter image provider, got %#v", providers)
	}
	models := goai.ListImageModels(goai.ImagesProviderOpenRouter)
	if len(models) != 28 {
		t.Fatalf("expected 28 image models, got %d", len(models))
	}
	m := goai.GetImageModel(goai.ImagesProviderOpenRouter, "black-forest-labs/flux.2-flex")
	if m == nil || m.Api != goai.ImagesApiOpenRouter || m.BaseURL == "" {
		t.Fatalf("expected generated openrouter image model, got %#v", m)
	}
}

func TestGenerateImagesOpenRouterHooksAndResponse(t *testing.T) {
	var sawAuth, sawPayload, sawResponse bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer test-key" {
			sawAuth = true
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["custom"] == "yes" {
			sawPayload = true
		}
		w.Header().Set("X-Test", "ok")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "resp-1",
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "prompt_tokens_details": map[string]any{"cached_tokens": 2}},
			"choices": []any{map[string]any{"message": map[string]any{
				"content": "caption",
				"images":  []any{map[string]any{"image_url": map[string]any{"url": "data:image/png;base64,aGk="}}},
			}}},
		})
	}))
	defer server.Close()

	model := &goai.ImagesModel{ID: "img", Api: goai.ImagesApiOpenRouter, Provider: goai.ImagesProviderOpenRouter, BaseURL: server.URL, Output: []string{"image", "text"}}
	out, err := goai.GenerateImages(model, goai.ImagesContext{Input: []goai.ImageInput{{Type: "text", Text: "draw"}}}, &goai.ImagesOptions{
		APIKey: "test-key",
		Signal: context.Background(),
		OnPayload: func(payload map[string]any, model *goai.ImagesModel) (map[string]any, error) {
			payload["custom"] = "yes"
			return payload, nil
		},
		OnResponse: func(response goai.ImagesResponseMetadata, model *goai.ImagesModel) error {
			if response.Status == 200 && response.Headers["X-Test"] == "ok" {
				sawResponse = true
			}
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawAuth || !sawPayload || !sawResponse {
		t.Fatalf("missing hook/auth observations: auth=%v payload=%v response=%v", sawAuth, sawPayload, sawResponse)
	}
	if out.ResponseID != "resp-1" || len(out.Output) != 2 || out.Output[1].MimeType != "image/png" || out.Usage == nil || out.Usage.TotalTokens != 15 {
		t.Fatalf("unexpected image output: %#v", out)
	}
}
