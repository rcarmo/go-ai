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
	if len(models) < 1 {
		t.Fatalf("expected image models, got %d", len(models))
	}
	allModels := goai.ListImageModels("")
	if len(allModels) < len(models) {
		t.Fatalf("expected wildcard image model list to include provider models")
	}
	m := goai.GetImageModel(goai.ImagesProviderOpenRouter, "black-forest-labs/flux.2-flex")
	if m == nil || m.Api != goai.ImagesApiOpenRouter || m.BaseURL == "" {
		t.Fatalf("expected generated openrouter image model, got %#v", m)
	}
}

func TestGenerateImagesErrorPaths(t *testing.T) {
	out, err := goai.GenerateImages(nil, goai.ImagesContext{}, nil)
	if err != nil || out.StopReason != goai.StopReasonError || out.ErrorMessage == "" {
		t.Fatalf("expected nil-model error result, got out=%#v err=%v", out, err)
	}
	out, err = goai.GenerateImages(&goai.ImagesModel{ID: "m", Api: "missing", Provider: "p"}, goai.ImagesContext{}, nil)
	if err != nil || out.StopReason != goai.StopReasonError || out.ErrorMessage == "" {
		t.Fatalf("expected missing-provider error result, got out=%#v err=%v", out, err)
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
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "prompt_tokens_details": map[string]any{"cached_tokens": 3, "cache_write_tokens": 1}},
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
	if out.StopReason != goai.StopReasonStop || out.ResponseID != "resp-1" || out.Usage == nil || out.Usage.TotalTokens != 15 {
		t.Fatalf("unexpected image output: %#v", out)
	}
	var sawText, sawImage bool
	for _, item := range out.Output {
		if item.Type == "text" && item.Text == "caption" {
			sawText = true
		}
		if item.Type == "image" && item.MimeType == "image/png" && item.Data == "aGk=" {
			sawImage = true
		}
	}
	if !sawText || !sawImage {
		t.Fatalf("expected text and image output, got %#v", out.Output)
	}
}

func TestGenerateImagesOpenRouterValidationAndAbort(t *testing.T) {
	model := &goai.ImagesModel{ID: "img", Api: goai.ImagesApiOpenRouter, Provider: goai.ImagesProviderOpenRouter, BaseURL: "http://127.0.0.1", Output: []string{"image"}}
	out, err := goai.GenerateImages(model, goai.ImagesContext{}, &goai.ImagesOptions{APIKey: "test-key"})
	if err != nil || out.StopReason != goai.StopReasonError || out.ErrorMessage == "" {
		t.Fatalf("expected empty input error result, got out=%#v err=%v", out, err)
	}
	out, err = goai.GenerateImages(model, goai.ImagesContext{Input: []goai.ImageInput{{Type: "bogus"}}}, &goai.ImagesOptions{APIKey: "test-key"})
	if err != nil || out.StopReason != goai.StopReasonError || out.ErrorMessage == "" {
		t.Fatalf("expected unsupported input type error result, got out=%#v err=%v", out, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err = goai.GenerateImages(model, goai.ImagesContext{Input: []goai.ImageInput{{Type: "text", Text: "draw"}}}, &goai.ImagesOptions{APIKey: "test-key", Context: ctx})
	if err != nil || out.StopReason != goai.StopReasonAborted || out.ErrorMessage == "" {
		t.Fatalf("expected aborted result, got out=%#v err=%v", out, err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	model.BaseURL = server.URL
	out, err = goai.GenerateImages(model, goai.ImagesContext{Input: []goai.ImageInput{{Type: "text", Text: "draw"}}}, &goai.ImagesOptions{APIKey: "test-key", Context: ctx})
	if err != nil || out.StopReason != goai.StopReasonAborted || out.ErrorMessage == "" {
		t.Fatalf("expected client cancellation to abort without retry, got out=%#v err=%v", out, err)
	}
}

func TestGenerateImagesOpenRouterRetriesAndHookError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "try again", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"images": []any{}}}}})
	}))
	defer server.Close()

	model := &goai.ImagesModel{ID: "img", Api: goai.ImagesApiOpenRouter, Provider: goai.ImagesProviderOpenRouter, BaseURL: server.URL, Output: []string{"image"}}
	out, err := goai.GenerateImages(model, goai.ImagesContext{Input: []goai.ImageInput{{Type: "text", Text: "draw"}}}, &goai.ImagesOptions{APIKey: "test-key", MaxRetries: 1, MaxRetryDelayMs: 1})
	if err != nil || out.StopReason != goai.StopReasonStop || attempts != 2 {
		t.Fatalf("expected retry success, attempts=%d out=%#v err=%v", attempts, out, err)
	}

	out, err = goai.GenerateImages(model, goai.ImagesContext{}, &goai.ImagesOptions{APIKey: "test-key", OnPayload: func(payload map[string]any, model *goai.ImagesModel) (map[string]any, error) {
		return nil, context.Canceled
	}})
	if err != nil || out.StopReason != goai.StopReasonError || out.ErrorMessage == "" {
		t.Fatalf("expected payload hook error result, got out=%#v err=%v", out, err)
	}
}
