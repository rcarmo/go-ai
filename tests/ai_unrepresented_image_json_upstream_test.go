package goai_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

// Source: pi v1.7.0 packages/ai/test/images-models.test.ts.
func TestV170ImagesModelsPublicTypedCatalogPaths(t *testing.T) {
	const provider = goai.Provider("typed-public")
	runtime := goai.NewModelRuntime(goai.NewInMemoryModelsStore())
	runtime.SetProvider(typedEntryProvider{
		id: provider,
		entry: &goai.ModelsStoreEntry{
			Models:           []*goai.Model{{Provider: provider, ID: "c1"}, {Provider: provider, ID: "c2", Type: "chat"}, {Provider: provider, ID: "wrong-image-chat", Type: "image"}},
			ImageModels:      []*goai.ImageModel{{Provider: goai.ImageProvider(provider), ID: "i1"}, {Provider: goai.ImageProvider(provider), ID: "i2", Type: "image"}, {Provider: goai.ImageProvider(provider), ID: "wrong-chat", Type: "chat"}},
			ClassifierModels: []*goai.ClassifierModel{{Provider: goai.ClassifierProvider(provider), ID: "d1"}, {Provider: goai.ClassifierProvider(provider), ID: "wrong-chat", Type: "chat"}},
		},
	})

	if got := modelIDs(runtime.GetModels(provider)); !slices.Equal(got, []string{"c1", "c2"}) {
		t.Fatalf("chat models = %v", got)
	}
	if got := anyModelIDs(runtime.GetModelsOfType(goai.ModelTypeImage, string(provider))); !slices.Equal(got, []string{"i1", "i2"}) {
		t.Fatalf("image models = %v", got)
	}
	if got := anyModelIDs(runtime.GetModelsOfType(goai.ModelTypeClassifier, string(provider))); !slices.Equal(got, []string{"d1"}) {
		t.Fatalf("classifier models = %v", got)
	}
	if got := anyModelIDs(runtime.GetAllModels(string(provider))); !slices.Equal(got, []string{"c1", "c2", "d1", "i1", "i2"}) {
		t.Fatalf("all models = %v", got)
	}
	if runtime.GetModelOfType(goai.ModelTypeChat, string(provider), "c1") == nil ||
		runtime.GetModelOfType(goai.ModelTypeImage, string(provider), "i1") == nil ||
		runtime.GetModelOfType(goai.ModelTypeClassifier, string(provider), "d1") == nil {
		t.Fatal("typed lookups did not find their models")
	}
	if runtime.GetModelOfType(goai.ModelTypeImage, string(provider), "c1") != nil ||
		runtime.GetModelOfType(goai.ModelTypeChat, string(provider), "i1") != nil {
		t.Fatal("typed lookup crossed model types")
	}
	if goai.GetModelType(&goai.Model{Provider: provider, ID: "legacy"}) != goai.ModelTypeChat ||
		goai.GetModelType(&goai.ImageModel{Provider: goai.ImageProvider(provider), ID: "image"}) != goai.ModelTypeImage ||
		!goai.IsModelType(&goai.Model{Provider: provider, ID: "explicit", Type: "chat"}, goai.ModelTypeChat) {
		t.Fatal("model discriminants lost the legacy chat or typed image contract")
	}
}

// Source: pi v1.7.0 packages/ai/test/images-models.test.ts; a dynamic provider
// publishes image and chat records through the native entry contract.
func TestV170ImagesModelsPublicRefreshAndImageGeneration(t *testing.T) {
	const provider = goai.Provider("typed-refresh")
	store := goai.NewInMemoryModelsStore()
	impl := &typedEntryProvider{id: provider, entry: &goai.ModelsStoreEntry{
		Models:      []*goai.Model{{Provider: provider, ID: "chat"}},
		ImageModels: []*goai.ImageModel{{Provider: goai.ImageProvider(provider), ID: "listed", Type: "image"}},
	}}
	runtime := goai.NewModelRuntime(store)
	runtime.SetProvider(impl)
	if result := runtime.Refresh(t.Context(), true); result.Aborted || len(result.Errors) != 0 {
		t.Fatalf("refresh = %+v", result)
	}
	if got := anyModelIDs(runtime.GetAllModels(string(provider))); !slices.Equal(got, []string{"chat", "listed"}) {
		t.Fatalf("refreshed all models = %v", got)
	}
	cached, err := store.Read(provider)
	if err != nil || cached == nil || len(cached.ImageModels) != 1 || cached.ImageModels[0].ID != "listed" {
		t.Fatalf("typed cache = %#v, %v", cached, err)
	}

	const api = images.ImagesApi("typed-test-images")
	images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: api, GenerateImages: func(model *images.ImagesModel, ctx images.ImagesContext, options *images.ImagesOptions) (*images.AssistantImages, error) {
		if len(ctx.Input) != 1 || ctx.Input[0].Text != "draw" || options == nil || options.APIKey != "explicit" {
			t.Fatalf("image call = model=%#v context=%#v options=%#v", model, ctx, options)
		}
		return &images.AssistantImages{Api: api, Provider: model.Provider, Model: model.ID, Output: []images.ImageOutput{{Type: "image", Data: "aGk=", MimeType: "image/png"}}, StopReason: goai.StopReasonStop}, nil
	}})
	t.Cleanup(func() { images.UnregisterImagesApiProvider(api) })
	result, err := images.GenerateImages(&images.ImagesModel{ID: "generated", Api: api, Provider: "typed-refresh"}, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}, &images.ImagesOptions{APIKey: "explicit"})
	if err != nil || result.StopReason != goai.StopReasonStop || len(result.Output) != 1 || result.Output[0].Type != "image" {
		t.Fatalf("generated = %#v, %v", result, err)
	}
}

// Source: pi v1.7.0 packages/ai/test/images.test.ts. The native public path is
// exercised offline with the OpenRouter HTTP shape, including image input.
func TestV170ImagesPublicTextAndImageInput(t *testing.T) {
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"img_1","choices":[{"message":{"images":[{"image_url":"data:image/png;base64,aGk="}]}}]}`)
	}))
	defer server.Close()
	model := &images.ImagesModel{ID: "image-model", Name: "Image Model", Api: images.ImagesApiOpenRouter, Provider: images.ImagesProviderOpenRouter, BaseURL: server.URL, Input: []string{"text", "image"}, Output: []string{"image"}}
	got, err := images.GenerateImages(model, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "variation"}, {Type: "image", Data: "aW4=", MimeType: "image/png"}}}, &images.ImagesOptions{APIKey: "test"})
	if err != nil || got.StopReason != goai.StopReasonStop || len(got.Output) != 1 || got.Output[0].Data != "aGk=" || got.Timestamp == 0 {
		t.Fatalf("result = %#v, %v", got, err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %#v", payload["messages"])
	}
	content := messages[0].(map[string]any)["content"].([]any)
	if len(content) != 2 || content[0].(map[string]any)["type"] != "text" || content[1].(map[string]any)["type"] != "image_url" {
		t.Fatalf("image input payload = %#v", content)
	}
}

// Source: pi v1.7.0 packages/ai/test/image-tool-result.test.ts. This controls
// the deterministic schema and mixed-content portion, not live model quality.
func TestV170ImageToolResultPublicTransformAndJSON(t *testing.T) {
	vision := &goai.Model{ID: "vision", Provider: "p", Api: "test", Input: []string{"text", "image"}}
	messages := []goai.Message{
		{Role: goai.RoleAssistant, Provider: vision.Provider, Api: vision.Api, Model: vision.ID, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "call_1", Name: "get_circle", Arguments: map[string]any{}}}},
		{Role: goai.RoleToolResult, ToolCallID: "call_1", ToolName: "get_circle", Content: []goai.ContentBlock{{Type: "text", Text: "diameter 100 pixels"}, {Type: "image", Data: "aGk=", MimeType: "image/png"}}},
	}
	got := goai.TransformMessages(messages, vision)
	if len(got) != 2 || len(got[1].Content) != 2 || got[1].Content[1].Type != "image" || got[1].Content[1].Data != "aGk=" {
		t.Fatalf("vision tool result = %#v", got)
	}
	textOnly := *vision
	textOnly.Input = []string{"text"}
	downgraded := goai.TransformMessages(messages, &textOnly)
	if len(downgraded[1].Content) != 2 || downgraded[1].Content[1].Type != "text" || !strings.Contains(downgraded[1].Content[1].Text, "tool image omitted") {
		t.Fatalf("text-only tool result = %#v", downgraded)
	}
	wire, err := json.Marshal(got[1])
	if err != nil || !strings.Contains(string(wire), `"content":[{"type":"text","text":"diameter 100 pixels"},{"type":"image","data":"aGk=","mimeType":"image/png"}]`) {
		t.Fatalf("tool result wire = %s, %v", wire, err)
	}
}

// Source: pi v1.7.0 packages/ai/test/interleaved-thinking.test.ts. The public
// Anthropic transport proves thinking before a tool call is replayed with the
// tool result and a second response can contain thinking plus final text.
func TestV170InterleavedThinkingPublicReplay(t *testing.T) {
	requests := make(chan map[string]any, 2)
	call := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests <- body
		w.Header().Set("Content-Type", "text/event-stream")
		call++
		if call == 1 {
			_, _ = io.WriteString(w, anthropicSSE([]string{
				`{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":1}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"first plan","signature":"sig1"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"toolu_1","name":"calculator","input":{}}}`,
				`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"a\":328,\"b\":29,\"operation\":\"multiply\"}"}}`,
				`{"type":"content_block_stop","index":1}`,
				`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
				`{"type":"message_stop"}`,
			}))
			return
		}
		_, _ = io.WriteString(w, anthropicSSE([]string{
			`{"type":"message_start","message":{"id":"m2","usage":{"input_tokens":3}}}`,
			`{"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"check the result","signature":"sig2"}}`,
			`{"type":"content_block_stop","index":0}`,
			`{"type":"content_block_start","index":1,"content_block":{"type":"text","text":"9512"}}`,
			`{"type":"content_block_stop","index":1}`,
			`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}`,
			`{"type":"message_stop"}`,
		}))
	}))
	defer server.Close()

	model := &goai.Model{ID: "claude-test", Name: "Claude", Api: goai.ApiAnthropicMessages, Provider: goai.ProviderAnthropic, BaseURL: server.URL, Reasoning: true, Input: []string{"text"}, ContextWindow: 200000, MaxTokens: 1000}
	zero, high := 0, goai.ThinkingHigh
	ctx := &goai.Context{Messages: []goai.Message{goai.UserMessage("calculate")}, Tools: []goai.Tool{{Name: "calculator", Description: "calculate", Parameters: json.RawMessage(`{"type":"object"}`)}}}
	options := &goai.StreamOptions{APIKey: "test", MaxRetries: &zero, Reasoning: &high}
	first, err := goai.Complete(t.Context(), model, ctx, options)
	if err != nil || first.StopReason != goai.StopReasonToolUse || !reflect.DeepEqual(contentTypes(first.Content), []string{"thinking", "toolCall"}) {
		t.Fatalf("first = %#v, %v", first, err)
	}
	ctx.Messages = append(ctx.Messages, *first, goai.Message{Role: goai.RoleToolResult, ToolCallID: "toolu_1", ToolName: "calculator", Content: []goai.ContentBlock{{Type: "text", Text: "9512 or 19024"}}})
	second, err := goai.Complete(t.Context(), model, ctx, options)
	if err != nil || second.StopReason != goai.StopReasonStop || !reflect.DeepEqual(contentTypes(second.Content), []string{"thinking", "text"}) {
		t.Fatalf("second = %#v, %v", second, err)
	}
	firstBody, secondBody := <-requests, <-requests
	if firstBody["thinking"] == nil || secondBody["thinking"] == nil {
		t.Fatalf("thinking request settings: first=%#v second=%#v", firstBody["thinking"], secondBody["thinking"])
	}
	secondMessages := secondBody["messages"].([]any)
	encoded, _ := json.Marshal(secondMessages)
	var sawThinking, sawToolResult bool
	for _, message := range secondMessages {
		blocks, _ := message.(map[string]any)["content"].([]any)
		for _, rawBlock := range blocks {
			block, _ := rawBlock.(map[string]any)
			if block["type"] == "thinking" && block["thinking"] == "first plan" && block["signature"] == "sig1" {
				sawThinking = true
			}
			if block["type"] == "tool_result" && block["tool_use_id"] == "toolu_1" {
				sawToolResult = true
			}
		}
	}
	if !sawThinking || !sawToolResult {
		t.Fatalf("second request lost thinking/tool result: %s", encoded)
	}
}

func anthropicSSE(events []string) string {
	var b strings.Builder
	for _, event := range events {
		name := strings.SplitN(strings.TrimPrefix(event, `{"type":"`), `"`, 2)[0]
		b.WriteString("event: ")
		b.WriteString(name)
		b.WriteString("\ndata: ")
		b.WriteString(event)
		b.WriteString("\n\n")
	}
	return b.String()
}

func contentTypes(blocks []goai.ContentBlock) []string {
	out := make([]string, 0, len(blocks))
	for _, block := range blocks {
		out = append(out, block.Type)
	}
	return out
}

func modelIDs(models []*goai.Model) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.ID)
	}
	slices.Sort(out)
	return out
}

func anyModelIDs(models []any) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		switch value := model.(type) {
		case *goai.Model:
			out = append(out, value.ID)
		case *goai.ImageModel:
			out = append(out, value.ID)
		case *goai.ClassifierModel:
			out = append(out, value.ID)
		}
	}
	slices.Sort(out)
	return out
}

type typedEntryProvider struct {
	id    goai.Provider
	entry *goai.ModelsStoreEntry
}

func (p typedEntryProvider) ID() string                               { return string(p.id) }
func (p typedEntryProvider) StaticModels() []*goai.Model              { return p.entry.Models }
func (p typedEntryProvider) StaticModelEntry() *goai.ModelsStoreEntry { return p.entry }
func (p typedEntryProvider) RefreshModels(goai.ModelRefreshContext) ([]*goai.Model, error) {
	return p.entry.Models, nil
}
func (p typedEntryProvider) RefreshModelEntry(goai.ModelRefreshContext) (*goai.ModelsStoreEntry, error) {
	return p.entry, nil
}

var _ goai.DynamicModelStaticEntryProvider = typedEntryProvider{}
var _ goai.DynamicModelEntryProvider = typedEntryProvider{}
