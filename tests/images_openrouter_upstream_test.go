package goai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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

func openRouterImageFixtureModel(id string, pricing goai.ModelCost) *images.ImagesModel {
	return &images.ImagesModel{ID: id, Api: images.ImagesApiOpenRouter, Provider: images.ImagesProviderOpenRouter, BaseURL: "https://openrouter.ai/api/v1", Input: []string{"text", "image"}, Output: []string{"image", "text"}, Cost: pricing}
}

func openRouterImageFixtureCall(t *testing.T, model *images.ImagesModel, prompt, response string, opts *images.ImagesOptions) (*images.AssistantImages, map[string]any) {
	t.Helper()
	var decoded map[string]any
	got := generateImagesWithTransport(t, model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: prompt}}}, opts, func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		if err := json.NewDecoder(req.Body).Decode(&decoded); err != nil {
			return nil, err
		}
		return imageJSONResponse(200, response), nil
	})
	return got, decoded
}

func TestOpenRouterImageConfig(t *testing.T) {
	model := openRouterImageFixtureModel("google/gemini-2.5-flash-image", goai.ModelCost{Input: .3, Output: 2.5, CacheRead: .03, CacheWrite: .083333})
	got, payload := openRouterImageFixtureCall(t, model, "A landscape", `{"choices":[{"message":{"images":[{"image_url":"data:image/png;base64,AAAA"}]}}]}`, &images.ImagesOptions{APIKey: "test", AspectRatio: new("16:9"), Size: new("2K")})
	if got.StopReason != goai.StopReasonStop {
		t.Fatalf("result=%+v", got)
	}
	want := map[string]any{"aspect_ratio": "16:9", "image_size": "2K"}
	if !reflect.DeepEqual(payload["image_config"], want) {
		t.Errorf("image_config=%#v; want %#v", payload["image_config"], want)
	}
}

func TestOpenRouterImageFinishErrorCost(t *testing.T) {
	model := openRouterImageFixtureModel("openai/gpt-5-image", goai.ModelCost{Input: 10, Output: 10, CacheRead: 1.25})
	got, _ := openRouterImageFixtureCall(t, model, "A compass rose", `{"id":"gen-err-1","usage":{"prompt_tokens":12,"completion_tokens":556,"cost":0,"is_byok":false,"cost_details":{"upstream_inference_cost":0.00568,"upstream_inference_prompt_cost":0.00012,"upstream_inference_completions_cost":0.00556}},"choices":[{"finish_reason":"error","native_finish_reason":null,"message":{"role":"assistant","content":null}}]}`, &images.ImagesOptions{APIKey: "test"})
	assertImageResultEmpty(t, got, goai.StopReasonError)
	if got.ErrorMessage != "Provider finish_reason: error" || got.ResponseID != "gen-err-1" {
		t.Errorf("identity or error=%+v", got)
	}
	if got.Usage == nil || got.Usage.Cost.Total != 0 || got.Usage.Cost.TotalFromProvider == nil || !*got.Usage.Cost.TotalFromProvider {
		t.Errorf("reported zero cost=%+v", got.Usage)
	}
}

func TestOpenRouterImageChoiceError(t *testing.T) {
	for _, row := range []struct{ name, response, want string }{
		{"no-choices", `{"id":"gen-empty","error":{"message":null,"code":null},"choices":[]}`, "Provider returned no choices"},
		{"top-level-fallback", `{"id":"gen-choice-err","error":{"code":503,"message":"Top-level error"},"choices":[{"finish_reason":"error","error":{"message":"   ","code":""},"message":{"role":"assistant","content":null}}]}`, "Top-level error"},
		{"native-reason", `{"id":"gen-native-reason","choices":[{"finish_reason":"error","native_finish_reason":"IMAGE_SAFETY","message":{"role":"assistant","content":null}}]}`, "Provider finish_reason: error (native_finish_reason: IMAGE_SAFETY)"},
	} {
		t.Run(row.name, func(t *testing.T) {
			got, _ := openRouterImageFixtureCall(t, openRouterImageFixtureModel("openai/gpt-5-image", goai.ModelCost{Input: 10, Output: 10, CacheRead: 1.25}), "A compass rose", row.response, &images.ImagesOptions{APIKey: "test"})
			if got.StopReason != goai.StopReasonError || got.ErrorMessage != row.want {
				t.Errorf("result=%+v; want error %q", got, row.want)
			}
		})
	}
}

func TestOpenRouterImageReportedCost(t *testing.T) {
	model := openRouterImageFixtureModel("google/gemini-2.5-flash-image", goai.ModelCost{Input: .3, Output: 2.5, CacheRead: .03, CacheWrite: .083333})
	got, _ := openRouterImageFixtureCall(t, model, "A compass rose", `{"id":"gen-ok-1","usage":{"prompt_tokens":15,"completion_tokens":1290,"cost":0.042,"is_byok":false,"cost_details":null},"choices":[{"finish_reason":"stop","message":{"content":null,"images":[{"image_url":"data:image/png;base64,ZmFrZS1wbmc="}]}}]}`, &images.ImagesOptions{APIKey: "test"})
	if got.StopReason != goai.StopReasonStop || got.Usage == nil {
		t.Fatalf("result=%+v", got)
	}
	if got.Usage.Cost.Total != .042 || got.Usage.Cost.TotalFromProvider == nil || !*got.Usage.Cost.TotalFromProvider {
		t.Errorf("reported cost=%+v", got.Usage.Cost)
	}
	if got.Usage.Reasoning != 0 || !got.Usage.ReasoningSet || got.Usage.ImageOutputSet {
		t.Errorf("usage presence=%+v", got.Usage)
	}
}
