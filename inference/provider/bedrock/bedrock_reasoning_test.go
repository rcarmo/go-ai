package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
)

func bedrockReasoningModels(t *testing.T) map[string]goai.Model {
	t.Helper()
	data, err := os.ReadFile("testdata/bedrock_reasoning_v170.json")
	if err != nil {
		t.Fatal(err)
	}
	// Upstream's compat is exposed via Go's provider-specific bedrockCompat field.
	var fixtures map[string]struct {
		goai.Model
		Compat *goai.BedrockCompat `json:"compat"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	models := make(map[string]goai.Model, len(fixtures))
	for id, fixture := range fixtures {
		model := fixture.Model
		model.Api = goai.ApiBedrockConverseStream
		model.Provider = "amazon-bedrock"
		model.BedrockCompat = fixture.Compat
		models[id] = model
	}
	return models
}

func bedrockPayload(t *testing.T, model *goai.Model, messages []goai.Message, reasoning *goai.ThinkingLevel) *bedrockruntime.ConverseStreamInput {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	captured := errors.New("payload captured")
	var input *bedrockruntime.ConverseStreamInput
	var calls int
	stream := goai.Stream(ctx, model, &goai.Context{Messages: messages}, &goai.StreamOptions{
		Reasoning:      reasoning,
		CacheRetention: "none",
		Region:         "us-east-1",
		Env:            goai.ProviderEnv{"AWS_ACCESS_KEY_ID": "offline", "AWS_SECRET_ACCESS_KEY": "offline"},
		OnPayload: func(payload any, _ *goai.Model) (any, error) {
			calls++
			input, _ = payload.(*bedrockruntime.ConverseStreamInput)
			return nil, captured
		},
	})
	var terminal error
	for event := range stream {
		if e, ok := event.(*goai.ErrorEvent); ok {
			terminal = e.Err
		}
	}
	if calls != 1 || input == nil || !errors.Is(terminal, captured) {
		t.Fatalf("payload calls = %d, captured = %t, terminal = %v", calls, input != nil, terminal)
	}
	return input
}

func TestBedrockFlatReasoningEffort(t *testing.T) {
	const (
		globalKimi = "global.moonshotai.kimi-k3.1"
		usKimi     = "us.moonshotai.kimi-k3.1"
		globalGLM  = "global.zai.glm-5.3"
		usGLM      = "us.zai.glm-5.3"
	)
	models := bedrockReasoningModels(t)
	levels := []goai.ModelThinkingLevel{"off", "low", "medium", "high", "xhigh", "max"}
	for _, tc := range []struct {
		id   string
		want []goai.ModelThinkingLevel
	}{{globalKimi, levels}, {usKimi, levels}, {globalGLM, levels[1:]}} {
		t.Run("levels/"+tc.id, func(t *testing.T) {
			model := models[tc.id]
			if got := goai.GetSupportedThinkingLevels(&model); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("levels = %v, want %v", got, tc.want)
			}
		})
	}

	assertPayload := func(t *testing.T, model goai.Model, level *goai.ThinkingLevel, want string) {
		t.Helper()
		fields := bedrockPayload(t, &model, []goai.Message{goai.UserMessage("Hello")}, level).AdditionalModelRequestFields
		if want == "" {
			if fields != nil {
				got, err := fields.MarshalSmithyDocument()
				t.Fatalf("additional model request fields = %s (%v), want absent", got, err)
			}
			return
		}
		if fields == nil {
			t.Fatalf("additional model request fields absent, want reasoning_effort %q", want)
		}
		got, err := fields.MarshalSmithyDocument()
		if err != nil {
			t.Fatal(err)
		}
		var decoded map[string]any
		if err := json.Unmarshal(got, &decoded); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(decoded, map[string]any{"reasoning_effort": want}) {
			t.Fatalf("additional model request fields = %s, want only reasoning_effort %q", got, want)
		}
	}
	for _, level := range []goai.ThinkingLevel{goai.ThinkingLow, goai.ThinkingMedium, goai.ThinkingHigh, goai.ThinkingXHigh, goai.ThinkingMax} {
		for _, id := range []string{globalKimi, globalGLM} {
			t.Run("effort/"+string(level)+"/"+id, func(t *testing.T) {
				assertPayload(t, models[id], &level, string(level))
			})
		}
	}
	t.Run("off/mapped", func(t *testing.T) { assertPayload(t, models[usKimi], nil, "none") })
	t.Run("off/hidden", func(t *testing.T) { assertPayload(t, models[usGLM], nil, "") })
	t.Run("no-map", func(t *testing.T) {
		model := models[globalKimi]
		model.ThinkingLevelMap = nil
		medium := goai.ThinkingMedium
		assertPayload(t, model, &medium, "")
	})
}

func TestBedrockReasoningReplay(t *testing.T) {
	models := bedrockReasoningModels(t)
	nova := goai.Model{Api: goai.ApiBedrockConverseStream, Provider: "amazon-bedrock", ID: "amazon.nova-lite-v1:0", Name: "Nova Lite", Reasoning: true}
	for _, tc := range []struct {
		name          string
		model         goai.Model
		blocks        []goai.ContentBlock
		wantReasoning bool
	}{
		{name: "default", model: nova, wantReasoning: true},
		{name: "kimi-k3", model: models["global.moonshotai.kimi-k3"]},
		{name: "kimi-k3.1", model: models["us.moonshotai.kimi-k3.1"]},
		{name: "kimi-redacted", model: models["us.moonshotai.kimi-k3.1"], blocks: []goai.ContentBlock{{Type: "thinking", Redacted: true, ThinkingSignature: "AQI="}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const thinking = "I should read it."
			model := tc.model
			if !tc.wantReasoning {
				// The original checks the catalog property before the replay payload.
				// Marshal the public compatibility type so the baseline can run before
				// that type has a field for the property.
				data, err := json.Marshal(model.BedrockCompat)
				var compat map[string]any
				if err != nil || json.Unmarshal(data, &compat) != nil || compat["supportsReasoningReplay"] != false {
					t.Errorf("compat = %s (%v), want supportsReasoningReplay=false", data, err)
				}
			}
			blocks := tc.blocks
			if blocks == nil {
				blocks = []goai.ContentBlock{{Type: "thinking", Thinking: thinking}}
			}
			blocks = append(blocks, goai.ContentBlock{Type: "toolCall", ID: "tool-1", Name: "read", Arguments: map[string]any{"path": "a.txt"}})
			messages := []goai.Message{
				goai.UserMessage("Read the file"),
				{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, StopReason: goai.StopReasonToolUse, Content: blocks},
				{Role: goai.RoleToolResult, ToolCallID: "tool-1", ToolName: "read", Content: []goai.ContentBlock{{Type: "text", Text: "hello"}}},
			}
			input := bedrockPayload(t, &model, messages, nil)
			if len(input.Messages) != 3 || input.Messages[1].Role != types.ConversationRoleAssistant {
				t.Fatalf("want user, assistant, tool-result messages: %#v", input.Messages)
			}
			content := input.Messages[1].Content
			if tc.wantReasoning {
				if len(content) != 2 {
					t.Fatalf("assistant blocks = %d, want reasoning and tool call", len(content))
				}
				block, ok := content[0].(*types.ContentBlockMemberReasoningContent)
				if !ok {
					t.Fatalf("first block = %T, want unsigned reasoningContent", content[0])
				}
				text, ok := block.Value.(*types.ReasoningContentBlockMemberReasoningText)
				if !ok || aws.ToString(text.Value.Text) != thinking || text.Value.Signature != nil {
					t.Fatalf("reasoning content = %#v, want unsigned %q", block.Value, thinking)
				}
				content = content[1:]
			}
			if len(content) != 1 {
				t.Fatalf("remaining assistant blocks = %d, want only the tool call", len(content))
			}
			tool, ok := content[0].(*types.ContentBlockMemberToolUse)
			if !ok || aws.ToString(tool.Value.ToolUseId) != "tool-1" || aws.ToString(tool.Value.Name) != "read" || tool.Value.Input == nil {
				t.Fatalf("tool use = %#v, want read/tool-1 with input", content[0])
			}
			data, err := tool.Value.Input.MarshalSmithyDocument()
			if err != nil || string(data) != `{"path":"a.txt"}` {
				t.Fatalf("tool input = %s (%v), want path a.txt", data, err)
			}
		})
	}
}
