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

func TestOpenRouterImagesReturnsTextPlusImagesInFinalOutput(t *testing.T) {
	var params map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&params); err != nil {
			t.Fatal(err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "img-1",
			"usage":   map[string]any{"prompt_tokens": 12, "completion_tokens": 34, "prompt_tokens_details": map[string]any{"cached_tokens": 0}},
			"choices": []any{map[string]any{"message": map[string]any{"content": "Here is your image.", "images": []any{map[string]any{"image_url": "data:image/png;base64,ZmFrZS1wbmc="}}}}},
		})
	}))
	defer server.Close()
	model := &images.ImagesModel{ID: "google/gemini-3.1-flash-image-preview", Provider: images.ImagesProviderOpenRouter, Api: images.ImagesApiOpenRouter, BaseURL: server.URL, Input: []string{"text", "image"}, Output: []string{"text", "image"}, Cost: goai.ModelCost{Input: 0.015, Output: 0.03}, Headers: map[string]string{"HTTP-Referer": "https://example.com"}}
	out, err := openrouter.GenerateImagesOpenRouter(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "Generate a dog"}}}, &images.ImagesOptions{APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.StopReason != goai.StopReasonStop || out.ResponseID != "img-1" {
		t.Fatalf("unexpected output: %#v", out)
	}
	if len(out.Output) != 2 || out.Output[0].Type != "text" || out.Output[0].Text != "Here is your image." || out.Output[1].Type != "image" || out.Output[1].MimeType != "image/png" || out.Output[1].Data != "ZmFrZS1wbmc=" {
		t.Fatalf("outputs=%#v", out.Output)
	}
	if params["stream"] != false {
		t.Fatalf("stream=%#v", params["stream"])
	}
	mods, _ := params["modalities"].([]any)
	if len(mods) != 2 || mods[0] != "image" || mods[1] != "text" {
		t.Fatalf("modalities=%#v", mods)
	}
	messages := params["messages"].([]any)
	content := messages[0].(map[string]any)["content"].([]any)
	first := content[0].(map[string]any)
	if first["type"] != "text" || first["text"] != "Generate a dog" {
		t.Fatalf("first content=%#v", first)
	}
}

func TestOpenRouterImagesPassesThroughAbortSignalAndReturnsAbortedResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	model := &images.ImagesModel{ID: "black-forest-labs/flux.2-pro", Provider: images.ImagesProviderOpenRouter, Api: images.ImagesApiOpenRouter, BaseURL: "https://example.invalid/v1", Input: []string{"text", "image"}, Output: []string{"image"}, Cost: goai.ModelCost{Input: 0.015, Output: 0.03}}
	out, err := openrouter.GenerateImagesOpenRouter(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "Generate a dog"}}}, &images.ImagesOptions{APIKey: "test", Signal: ctx})
	if err != nil {
		t.Fatal(err)
	}
	if out.StopReason != goai.StopReasonAborted || out.ErrorMessage != "Request aborted" {
		t.Fatalf("unexpected aborted output: %#v", out)
	}
}

func TestOpenRouterImagesGenerateImagesResolvesFinalAssistantImagesResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"images": []any{map[string]any{"image_url": "data:image/png;base64,ZmFrZS1wbmc="}}}}}})
	}))
	defer server.Close()
	model := &images.ImagesModel{ID: "black-forest-labs/flux.2-pro", Provider: images.ImagesProviderOpenRouter, Api: images.ImagesApiOpenRouter, BaseURL: server.URL, Input: []string{"text", "image"}, Output: []string{"image"}, Cost: goai.ModelCost{Input: 0.015, Output: 0.03}}
	out, err := images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "Generate a dog"}}}, &images.ImagesOptions{APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Output) == 0 || out.Output[0].Type != "image" {
		t.Fatalf("expected image output, got %#v", out)
	}
}
