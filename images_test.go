package goai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	"github.com/rcarmo/go-ai/images/openrouter"
)

func TestGenerateImagesOpenRouterMissingAPIKeyErrorMatchesUpstream(t *testing.T) {
	model := &images.ImagesModel{ID: "openrouter/image", Provider: images.ImagesProvider("openrouter"), Api: images.ImagesApi("openrouter-images"), BaseURL: "https://example.invalid/v1"}
	out, err := openrouter.GenerateImagesOpenRouter(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.ErrorMessage != "No API key for provider: openrouter" {
		t.Fatalf("unexpected missing-key image output: %#v", out)
	}
}

func TestImageAPIProviderRegistered(t *testing.T) {
	p := images.GetImagesApiProvider(images.ImagesApiOpenRouter)
	if p == nil {
		t.Fatal("expected openrouter images provider to be registered")
	}
}

func TestBuiltinImageModels(t *testing.T) {
	images.RegisterBuiltinImageModels()
	providers := images.ListImageProviders()
	foundOpenRouter := false
	for _, p := range providers {
		if p == images.ImagesProviderOpenRouter {
			foundOpenRouter = true
			break
		}
	}
	if !foundOpenRouter {
		t.Fatalf("expected openrouter image provider, got %#v", providers)
	}
	models := images.ListImageModels(images.ImagesProviderOpenRouter)
	if len(models) < 1 {
		t.Fatalf("expected image models, got %d", len(models))
	}
	allModels := images.ListImageModels("")
	if len(allModels) < len(models) {
		t.Fatalf("expected wildcard image model list to include provider models")
	}
	m := images.GetImageModel(images.ImagesProviderOpenRouter, "black-forest-labs/flux.2-flex")
	if m == nil || m.Api != images.ImagesApiOpenRouter || m.BaseURL == "" {
		t.Fatalf("expected generated openrouter image model, got %#v", m)
	}
	pro := images.GetImageModel(images.ImagesProviderOpenRouter, "google/gemini-3-pro-image")
	if pro == nil || pro.Cost.Input != 2 || pro.Cost.CacheWrite != 0.375 {
		t.Fatalf("expected Gemini 3 Pro Image metadata, got %#v", pro)
	}
	flash := images.GetImageModel(images.ImagesProviderOpenRouter, "google/gemini-3.1-flash-image")
	if flash == nil || flash.Cost.Output != 3 || len(flash.Output) != 2 {
		t.Fatalf("expected Gemini 3.1 Flash Image metadata, got %#v", flash)
	}
	wantIDs := []string{
		"black-forest-labs/flux.2-flex",
		"google/gemini-3.1-flash-lite-image",
		"openai/gpt-image-1",
		"openai/gpt-image-1-mini",
		"openai/gpt-image-2",
		"sourceful/riverflow-v2.5-pro",
	}
	if len(models) != 39 {
		t.Fatalf("openrouter image model count = %d, want 39 from pi-ai v0.81.1", len(models))
	}
	for _, id := range wantIDs {
		if got := images.GetImageModel(images.ImagesProviderOpenRouter, id); got == nil {
			t.Fatalf("missing pi-ai v0.80.6 image model %q", id)
		}
	}
	for _, removed := range []string{"sourceful/riverflow-v2-fast-preview", "sourceful/riverflow-v2-max-preview", "sourceful/riverflow-v2-standard-preview"} {
		if got := images.GetImageModel(images.ImagesProviderOpenRouter, removed); got != nil {
			t.Fatalf("stale image model %q still registered: %#v", removed, got)
		}
	}
}

func TestGenerateImagesErrorPaths(t *testing.T) {
	out, err := images.GenerateImages(nil, images.ImagesContext{}, nil)
	if err != nil || out.StopReason != goai.StopReasonError || out.ErrorMessage == "" {
		t.Fatalf("expected nil-model error result, got out=%#v err=%v", out, err)
	}
	out, err = images.GenerateImages(&images.ImagesModel{ID: "m", Api: "missing", Provider: "p"}, images.ImagesContext{}, nil)
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

	model := &images.ImagesModel{ID: "img", Api: images.ImagesApiOpenRouter, Provider: images.ImagesProviderOpenRouter, BaseURL: server.URL, Output: []string{"image", "text"}}
	out, err := images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}, &images.ImagesOptions{
		APIKey: "test-key",
		Signal: context.Background(),
		OnPayload: func(payload map[string]any, model *images.ImagesModel) (map[string]any, error) {
			payload["custom"] = "yes"
			return payload, nil
		},
		OnResponse: func(response images.ImagesResponseMetadata, model *images.ImagesModel) error {
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

func TestGenerateImagesOpenRouterUsesProviderEnvAPIKey(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"images": []any{}}}},
		})
	}))
	defer server.Close()

	model := &images.ImagesModel{ID: "img", Api: images.ImagesApiOpenRouter, Provider: images.ImagesProviderOpenRouter, BaseURL: server.URL, Output: []string{"image"}}
	out, err := images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}, &images.ImagesOptions{Env: goai.ProviderEnv{"OPENROUTER_API_KEY": "env-key"}})
	if err != nil || out.StopReason != goai.StopReasonStop {
		t.Fatalf("expected image request to succeed via opts.Env API key, got out=%#v err=%v", out, err)
	}
	if gotAuth != "Bearer env-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
}

func TestGenerateImagesOpenRouterPayloadParityAndAbort(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"images": []any{}}}}})
	}))
	defer server.Close()
	model := &images.ImagesModel{ID: "img", Api: images.ImagesApiOpenRouter, Provider: images.ImagesProviderOpenRouter, BaseURL: server.URL, Output: []string{"text"}}
	out, err := images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "bad\xed\xa0\x80"}, {Type: "bogus", MimeType: "image/png", Data: "aGk="}}}, &images.ImagesOptions{APIKey: "test-key"})
	if err != nil || out.StopReason != goai.StopReasonStop {
		t.Fatalf("expected request to succeed, got out=%#v err=%v", out, err)
	}
	modalities, _ := payload["modalities"].([]any)
	if len(modalities) != 2 || modalities[0] != "image" || modalities[1] != "text" {
		t.Fatalf("expected upstream modalities [image text], got %#v", payload["modalities"])
	}
	messages := payload["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	if got := content[0].(map[string]any)["text"]; got != "bad" {
		t.Fatalf("expected sanitized surrogate text, got %#v", got)
	}
	if content[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("expected non-text input to become image_url, got %#v", content[1])
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err = images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}, &images.ImagesOptions{APIKey: "test-key", Context: ctx})
	if err != nil || out.StopReason != goai.StopReasonAborted || out.ErrorMessage == "" {
		t.Fatalf("expected aborted result, got out=%#v err=%v", out, err)
	}

	abortServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer abortServer.Close()
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	model.BaseURL = abortServer.URL
	out, err = images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}, &images.ImagesOptions{APIKey: "test-key", Context: ctx})
	if err != nil || out.StopReason != goai.StopReasonAborted || out.ErrorMessage == "" {
		t.Fatalf("expected client cancellation to abort without retry, got out=%#v err=%v", out, err)
	}
}

func TestGenerateImagesOpenRouterRetriesAndHookError(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts == 1 {
			// Exercise Retry-After handling on a retryable provider status.
			w.Header().Set("Retry-After", "1")
			http.Error(w, "try again", http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"images": []any{}}}}})
	}))
	defer server.Close()

	model := &images.ImagesModel{ID: "img", Api: images.ImagesApiOpenRouter, Provider: images.ImagesProviderOpenRouter, BaseURL: server.URL, Output: []string{"image"}}
	out, err := images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}, &images.ImagesOptions{APIKey: "test-key", MaxRetries: 1, MaxRetryDelayMs: 1})
	if err != nil || out.StopReason != goai.StopReasonStop || attempts != 2 {
		t.Fatalf("expected retry success, attempts=%d out=%#v err=%v", attempts, out, err)
	}

	out, err = images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}, &images.ImagesOptions{APIKey: "test-key", OnPayload: func(payload map[string]any, model *images.ImagesModel) (map[string]any, error) {
		return nil, context.Canceled
	}})
	if err != nil || out.StopReason != goai.StopReasonError || out.ErrorMessage == "" {
		t.Fatalf("expected payload hook error result, got out=%#v err=%v", out, err)
	}
}
