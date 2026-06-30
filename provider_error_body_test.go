package goai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	"github.com/rcarmo/go-ai/images/openrouter"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	_ "github.com/rcarmo/go-ai/inference/provider/openairesponses"
)

func TestProviderErrorBodyPassthroughOpenRouterImagesSurfacesStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"blocked by gateway WAF"}`))
	}))
	defer server.Close()
	model := &images.ImagesModel{ID: "black-forest-labs/flux.2-pro", Provider: images.ImagesProviderOpenRouter, Api: images.ImagesApiOpenRouter, BaseURL: server.URL, Input: []string{"text", "image"}, Output: []string{"image"}}
	out, err := openrouter.GenerateImagesOpenRouter(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "Generate a dog"}}}, &images.ImagesOptions{APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if out.StopReason != goai.StopReasonError {
		t.Fatalf("stopReason=%q", out.StopReason)
	}
	if !strings.Contains(out.ErrorMessage, "403") || !strings.Contains(out.ErrorMessage, "blocked by gateway WAF") || out.ErrorMessage == "403 status code (no body)" {
		t.Fatalf("errorMessage=%q", out.ErrorMessage)
	}
}

func TestProviderErrorBodyPassthroughOpenAICompletionsSurfacesStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"blocked by gateway WAF"}`))
	}))
	defer server.Close()
	model := &goai.Model{ID: "test-model", Provider: goai.ProviderOpenRouter, Api: goai.ApiOpenAICompletions, BaseURL: server.URL}
	msg, err := goai.Complete(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{APIKey: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	if msg != nil && msg.ErrorMessage != "" {
		text += " " + msg.ErrorMessage
	}
	if !strings.Contains(text, "403") || !strings.Contains(text, "blocked by gateway WAF") {
		t.Fatalf("error=%q msg=%#v", err, msg)
	}
}

func TestProviderErrorBodyPassthroughOpenAIResponsesSurfacesStatusAndBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"blocked by gateway WAF"}`))
	}))
	defer server.Close()
	model := &goai.Model{ID: "gpt-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAIResponses, BaseURL: server.URL}
	msg, err := goai.Complete(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}}, &goai.StreamOptions{APIKey: "test"})
	if err == nil {
		t.Fatal("expected error")
	}
	text := err.Error()
	if msg != nil && msg.ErrorMessage != "" {
		text += " " + msg.ErrorMessage
	}
	if !strings.Contains(text, "403") || !strings.Contains(text, "blocked by gateway WAF") {
		t.Fatalf("error=%q msg=%#v", err, msg)
	}
}

func TestProviderErrorBodyPassthroughBedrockServiceExceptionShape(t *testing.T) {
	norm := goai.NormalizeProviderError(providerErr{msg: "UnknownError", status: 403, hasStatus: true, body: `{"message":"blocked by gateway WAF"}`, hasBody: true})
	formatted := goai.FormatProviderError(norm, "bedrock")
	if !strings.Contains(formatted, "403") || !strings.Contains(formatted, "blocked by gateway WAF") || strings.Contains(formatted, "Unknown: UnknownError") {
		t.Fatalf("formatted=%q", formatted)
	}
}
