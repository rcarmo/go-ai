package openairesponses

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestAzureOpenAIBaseURLNormalizesCognitiveServicesRootEndpoints(t *testing.T) {
	assertAzureBaseURL(t, "https://marc-quicktests-resource.cognitiveservices.azure.com", "https://marc-quicktests-resource.cognitiveservices.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLNormalizesAzureOpenAIRootEndpoints(t *testing.T) {
	assertAzureBaseURL(t, "https://my-resource.openai.azure.com", "https://my-resource.openai.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLNormalizesOpenAIPathToV1(t *testing.T) {
	assertAzureBaseURL(t, "https://my-resource.cognitiveservices.azure.com/openai", "https://my-resource.cognitiveservices.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLPreservesOpenAIV1Endpoints(t *testing.T) {
	assertAzureBaseURL(t, "https://my-resource.cognitiveservices.azure.com/openai/v1", "https://my-resource.cognitiveservices.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLPreservesExplicitNonAzureProxyPaths(t *testing.T) {
	assertAzureBaseURL(t, "https://my-proxy.example.com/v1", "https://my-proxy.example.com/v1")
}

func TestAzureOpenAIBaseURLStripsQueryParamsWhenNormalizingAzureHostURLs(t *testing.T) {
	assertAzureBaseURL(t, "https://my-resource.openai.azure.com/openai?api-version=2024-12-01", "https://my-resource.openai.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLPreservesQueryParamsOnNonAzureProxyURLs(t *testing.T) {
	assertAzureBaseURL(t, "https://my-proxy.example.com/v1?custom=true", "https://my-proxy.example.com/v1?custom=true")
}

func TestAzureOpenAIBaseURLNormalizesFoundryRootEndpoints(t *testing.T) {
	assertAzureBaseURL(t, "https://marc-quicktests-resource.ai.azure.com", "https://marc-quicktests-resource.ai.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLNormalizesResponsesPath(t *testing.T) {
	assertAzureBaseURL(t, "https://my-resource.services.ai.azure.com/openai/v1/responses", "https://my-resource.services.ai.azure.com/openai/v1")
}

func TestAzureOpenAIBaseURLThrowsOnInvalidURLs(t *testing.T) {
	_, _, _, err := goai.ResolveAzureConfig(&goai.Model{ID: "gpt-4o-mini"}, &goai.StreamOptions{AzureBaseURL: "not-a-url"})
	if err == nil || !strings.Contains(err.Error(), "Invalid Azure OpenAI base URL") {
		t.Fatalf("expected Invalid Azure OpenAI base URL error, got %v", err)
	}
}

func TestAzureOpenAIBaseURLClampsPromptCacheKeyToOpenAI64CharacterLimit(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{SessionID: strings.Repeat("x", 67)})
	if got, want := req.PromptCacheKey, strings.Repeat("x", 64); got != want {
		t.Fatalf("prompt_cache_key = %q, want %q", got, want)
	}
}

func TestAzureOpenAIBaseURLDisablesServerSideResponseStorage(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses}
	req := buildRequest(model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, nil)
	if req.Store {
		t.Fatal("store = true, want false")
	}
}

func TestAzureOpenAIBaseURLBuildsCorrectDefaultURLFromResourceName(t *testing.T) {
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses}
	baseURL, _, _, err := resolveAzureResponsesConfig(model, &goai.StreamOptions{Env: goai.ProviderEnv{"AZURE_OPENAI_RESOURCE_NAME": "my-resource"}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.TrimSuffix(baseURL, "/deployments/gpt-4o-mini"), "https://my-resource.openai.azure.com/openai/v1"; got != want {
		t.Fatalf("baseURL = %q, want %q", got, want)
	}
}

func TestAzureOpenAIResponsesHonorsStrictModeFalse(t *testing.T) {
	strict := false
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses, ResponsesCompat: &goai.OpenAIResponsesCompat{SupportsStrictMode: &strict}}
	ctx := &goai.Context{
		Messages: []goai.Message{goai.UserMessage("hello")},
		Tools: []goai.Tool{{
			Name:                "preferred",
			Description:         "Preferred constrained tool",
			Parameters:          json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"]}`),
			ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"},
		}},
	}
	req := buildRequest(model, ctx, nil)
	if len(req.Tools) != 1 || req.Tools[0].Strict != nil {
		t.Fatalf("supportsStrictMode=false emitted strict: %#v", req.Tools)
	}
}

func TestAzureOpenAIResponsesForwardsRawEventsBeforeCompletion(t *testing.T) {
	transport := &azureCaptureTransport{body: strings.Join([]string{
		`data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_azure"}}`,
		`data: {"type":"response.completed","sequence_number":1,"response":{"id":"resp_azure","status":"completed"}}`,
		`data: [DONE]`,
	}, "\n\n") + "\n\n"}
	var received []map[string]interface{}
	var eventModels []*goai.Model
	model := &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses}
	var done *goai.DoneEvent
	for event := range streamResponses(context.Background(), model, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{
		APIKey:        "test-key",
		AzureBaseURL:  "https://my-resource.openai.azure.com",
		HTTPTransport: transport,
		OnProviderStreamEvent: func(event interface{}, callbackModel *goai.Model) error {
			if raw, ok := event.(map[string]interface{}); ok {
				received = append(received, raw)
			}
			eventModels = append(eventModels, callbackModel)
			return nil
		},
	}) {
		switch event := event.(type) {
		case *goai.DoneEvent:
			done = event
		case *goai.ErrorEvent:
			t.Fatal(event.Err)
		}
	}
	if len(received) != 2 || received[0]["type"] != "response.created" || received[1]["type"] != "response.completed" {
		t.Fatalf("raw events = %#v", received)
	}
	if !reflect.DeepEqual(eventModels, []*goai.Model{model, model}) {
		t.Fatalf("callback models = %#v", eventModels)
	}
	if done == nil || done.Message.ResponseID != "resp_azure" {
		t.Fatalf("completion = %#v", done)
	}
}

func TestAzureOpenAIResponsesUserAgentHeaders(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{name: "default", want: goai.FormatUserAgent(nil)},
		{name: "explicit", headers: map[string]string{"User-Agent": "custom-agent"}, want: "custom-agent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			transport := &azureCaptureTransport{}
			for event := range streamResponses(context.Background(), &goai.Model{ID: "gpt-4o-mini", Provider: goai.ProviderAzureOpenAI, Api: goai.ApiAzureOpenAIResponses}, &goai.Context{Messages: []goai.Message{goai.UserMessage("hello")}}, &goai.StreamOptions{
				APIKey: "test-key", AzureBaseURL: "https://my-resource.openai.azure.com", Headers: tc.headers, HTTPTransport: transport,
			}) {
				if failure, ok := event.(*goai.ErrorEvent); ok {
					t.Fatal(failure.Err)
				}
			}
			if got := transport.request.Header.Get("User-Agent"); got != tc.want {
				t.Fatalf("User-Agent = %q, want %q", got, tc.want)
			}
		})
	}
}

func assertAzureBaseURL(t *testing.T, input, want string) {
	t.Helper()
	got, _, _, err := goai.ResolveAzureConfig(&goai.Model{ID: "gpt-4o-mini"}, &goai.StreamOptions{AzureBaseURL: input})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("ResolveAzureConfig(%q) = %q, want %q", input, got, want)
	}
}
