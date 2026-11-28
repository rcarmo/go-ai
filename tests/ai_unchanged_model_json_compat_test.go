package goai_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

// Controls for the unchanged pi v1.7.0 model/json compatibility originals.
func TestAIModelJSONUnchangedStreamingAndCompletedArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want map[string]any
	}{
		{"invalid unicode escape", `{"text":"\url{x}"}`, map[string]any{"text": `\url{x}`}},
		{"invalid unicode escapes keep fields", `{"path":"a.tex","content":"\usepackage{x}","mode":"w"}`, map[string]any{"path": `a.tex`, "content": `\usepackage{x}`, "mode": "w"}},
		{"valid unicode escape", `{"text":"caf\u00e9 \url"}`, map[string]any{"text": "café \\url"}},
		{"cut off unicode escape", `{"text":"ab\u00`, map[string]any{"text": "ab"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parser := &jsonparse.StreamingJSONParser{}
			parser.Append(tc.raw)
			got, present := parser.Snapshot()
			if !present || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Snapshot() = %#v, %t; want %#v", got, present, tc.want)
			}
		})
	}

	value, failure := jsonparse.ParseToolCallArguments(`{"kind":"seek","note":"\url"}`)
	if failure != "" || !reflect.DeepEqual(value, map[string]any{"kind": "seek", "note": `\url`}) {
		t.Fatalf("repaired arguments = %#v, %q", value, failure)
	}
	if value, failure = jsonparse.ParseToolCallArguments(""); failure != "" || len(value) != 0 {
		t.Fatalf("empty arguments = %#v, %q", value, failure)
	}
	for _, raw := range []string{`{"action":"apply","actions":[{"kind":"seek","time":.5}]}`, `{"kind":"seek","clipId":"clip-fl`} {
		value, failure := jsonparse.ParseToolCallArguments(raw)
		if len(value) != 0 || !strings.HasSuffix(failure, raw) || !strings.Contains(failure, "\n\nReceived arguments:\n") {
			t.Fatalf("malformed %q = %#v, %q", raw, value, failure)
		}
	}
	value, failure = jsonparse.ParseToolCallArguments(`[1,2]`)
	if len(value) != 0 || failure != "Tool arguments must be a JSON object\n\nReceived arguments:\n[1,2]" {
		t.Fatalf("non-object = %#v, %q", value, failure)
	}
}

func TestAIModelJSONUnchangedTypedCatalogContracts(t *testing.T) {
	goai.RegisterBuiltinModels()
	for _, id := range []string{"grok-4.5", "grok-4.6", "grok-4.7", "grok-4.3"} {
		model := goai.GetModel(goai.ProviderXAI, id)
		if model == nil || model.Api != goai.ApiOpenAIResponses || model.ID != id || model.Provider != goai.ProviderXAI {
			t.Fatalf("XAI %s = %#v", id, model)
		}
	}
	for _, id := range []string{"gpt-6-astra", "gpt-6-sol", "gpt-6-luna"} {
		model := goai.GetModel(goai.ProviderGitHubCopilot, id)
		if model == nil || model.Api != goai.ApiOpenAIResponses {
			t.Fatalf("Copilot %s = %#v", id, model)
		}
		if id != "gpt-6-astra" && (model.ContextWindow != 1000000 || model.MaxTokens != 128000 || ptrValue(model.ThinkingLevelMap[goai.ThinkingOff]) != "none" || ptrValue(model.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingMax)]) != "max") {
			t.Fatalf("Copilot %s limits/map = %#v", id, model)
		}
	}

	ordinary := &goai.Model{ID: "ordinary", Provider: "test", Reasoning: true}
	if got := goai.GetSupportedThinkingLevels(ordinary); !reflect.DeepEqual(got, []goai.ModelThinkingLevel{"off", "minimal", "low", "medium", "high"}) {
		t.Fatalf("ordinary levels = %q", got)
	}
	if got := goai.ClampThinkingLevel(ordinary, goai.ModelThinkingLevel(goai.ThinkingMax)); got != goai.ModelThinkingLevel(goai.ThinkingHigh) {
		t.Fatalf("ordinary max clamp = %q", got)
	}
	high, maxLevel := "high", "max"
	ordinary.ThinkingLevelMap = map[goai.ModelThinkingLevel]*string{goai.ModelThinkingLevel(goai.ThinkingXHigh): nil, goai.ModelThinkingLevel(goai.ThinkingMax): &maxLevel}
	if got := goai.GetSupportedThinkingLevels(ordinary); !reflect.DeepEqual(got, []goai.ModelThinkingLevel{"off", "minimal", "low", "medium", "high", "max"}) {
		t.Fatalf("hole levels = %q", got)
	}
	if got := goai.ClampThinkingLevel(ordinary, goai.ModelThinkingLevel(goai.ThinkingXHigh)); got != goai.ModelThinkingLevel(goai.ThinkingMax) {
		t.Fatalf("hole clamp = %q", got)
	}
	ordinary.ThinkingLevelMap[goai.ModelThinkingLevel(goai.ThinkingHigh)] = &high
}

func TestAIModelJSONUnchangedRuntimeDropsUnknownTypes(t *testing.T) {
	store := goai.NewInMemoryModelsStore()
	runtime := goai.NewModelRuntime(store)
	const provider goai.Provider = "dynamic-types"
	providerImpl := modelTypeProvider{
		provider: provider,
		models: []*goai.Model{
			{Provider: provider, ID: "stored-chat", Type: "chat"},
			{Provider: provider, ID: "stored-embedding", Type: "embedding"},
			{Provider: provider, ID: "stored-video", Type: "video"},
		},
	}
	runtime.SetProvider(&providerImpl)
	if err := store.Write(provider, &goai.ModelsStoreEntry{Models: providerImpl.models}); err != nil {
		t.Fatal(err)
	}
	if result := runtime.Refresh(t.Context(), false); result.Aborted || len(result.Errors) != 0 {
		t.Fatalf("cache-only refresh = %+v", result)
	}
	if got := runtime.GetModels(provider); len(got) != 1 || got[0].ID != "stored-chat" {
		t.Fatalf("stored unknown types retained: %#v", got)
	}
	providerImpl.models = []*goai.Model{
		{Provider: provider, ID: "fetched-chat", Type: "chat"},
		{Provider: provider, ID: "fetched-video", Type: "video"},
	}
	result := runtime.Refresh(t.Context(), true)
	if result.Aborted || len(result.Errors) != 0 {
		t.Fatalf("network refresh = %+v", result)
	}
	if got := runtime.GetModels(provider); len(got) != 1 || got[0].ID != "fetched-chat" {
		t.Fatalf("fetched unknown types retained: %#v", got)
	}
	entry, err := store.Read(provider)
	if err != nil || entry == nil || len(entry.Models) != 1 || entry.Models[0].ID != "fetched-chat" {
		t.Fatalf("stored refresh = %#v, %v", entry, err)
	}
}

func TestAIModelJSONUnchangedMessageAndEntryPublicShapes(t *testing.T) {
	message := goai.Message{
		Role: goai.RoleToolResult, Content: []goai.ContentBlock{}, ToolCallID: "call-1", ToolName: "read",
		Details: map[string]any{"path": "a.ts", "items": []string{"first"}},
	}
	wire, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var decoded goai.Message
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Details == nil {
		t.Fatal("JSON detail object was lost")
	}

	model := &goai.Model{ID: "text-only", Provider: "test", Api: goai.ApiOpenAICompletions, Input: []string{"text"}}
	out := goai.TransformMessages([]goai.Message{{Role: goai.RoleUser}, {Role: goai.RoleAssistant}, {Role: goai.RoleToolResult}}, model)
	for i := range out {
		if out[i].Content == nil || len(out[i].Content) != 0 {
			t.Fatalf("content %d was not normalized: %#v", i, out[i].Content)
		}
	}

	runtime := goai.NewModelRuntime(nil)
	runtime.SetProvider(goai.StaticModelProvider{Provider: "entry", Models: []*goai.Model{{Provider: "entry", ID: "one"}}})
	if got := runtime.GetModels("entry"); len(got) != 1 || got[0].ID != "one" {
		t.Fatalf("lightweight runtime model entry = %#v", got)
	}
}

type modelTypeProvider struct {
	provider goai.Provider
	models   []*goai.Model
}

func (p *modelTypeProvider) ID() string                  { return string(p.provider) }
func (p *modelTypeProvider) StaticModels() []*goai.Model { return nil }
func (p *modelTypeProvider) RefreshModels(goai.ModelRefreshContext) ([]*goai.Model, error) {
	return p.models, nil
}
