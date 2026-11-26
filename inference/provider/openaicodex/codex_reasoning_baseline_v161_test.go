package openaicodex

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// v1.6.1 openai-codex-stream.test.ts:1100–1164. The retained assistant
// messages come from actual completed SSE requests, not fabricated markers.
func TestSourceV161CodexReasoningBaseline(t *testing.T) {
	for _, name := range []string{"retained-level-changes", "requested-model-only", "simple-off"} {
		t.Run(name, func(t *testing.T) {
			url, requests := captureCodexSSEProjection(t,
				`{"type":"response.output_item.added","item":{"type":"message","id":"msg_1","role":"assistant","status":"in_progress","content":[]}}`,
				`{"type":"response.content_part.added","part":{"type":"output_text","text":""}}`,
				`{"type":"response.output_text.delta","delta":"Hello"}`,
				`{"type":"response.output_item.done","item":{"type":"message","id":"msg_1","role":"assistant","status":"completed","content":[{"type":"output_text","text":"Hello"}]}}`,
				`{"type":"response.completed","response":{"status":"completed"}}`,
			)
			yes, none := true, "none"
			model := &goai.Model{ID: "gpt-6.2-sol", Provider: goai.ProviderOpenAICodex, Api: goai.ApiOpenAICodexResponses,
				Reasoning: true, BaseURL: url, ContextWindow: 400_000, MaxTokens: 128_000,
				ResponsesCompat:  &goai.OpenAIResponsesCompat{SupportsMidConvoEffort: &yes},
				ThinkingLevelMap: map[goai.ModelThinkingLevel]*string{goai.ThinkingOff: &none},
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			provider := goai.GetApiProvider(model.Api)
			var bodies []map[string]any
			run := func(target *goai.Model, messages []goai.Message, effort string) goai.Message {
				t.Helper()
				zero := 0
				opts := &goai.StreamOptions{APIKey: payloadJWT, Transport: goai.TransportSSE, MaxRetries: &zero}
				stream, marker := provider.Stream, effort
				if effort == "" {
					stream, marker = provider.StreamSimple, "none"
				} else {
					level := goai.ThinkingLevel(effort)
					opts.Reasoning = &level
				}
				var result *goai.Message
				for event := range stream(ctx, target, &goai.Context{Messages: messages}, opts) {
					switch e := event.(type) {
					case *goai.ErrorEvent:
						t.Fatal(e.Err)
					case *goai.DoneEvent:
						result = e.Message
					}
				}
				if result == nil || result.StopReason != goai.StopReasonStop || goai.ContentText(result.Content) != "Hello" {
					t.Fatalf("result = %+v; want a retained Hello reply", result)
				}
				if result.ProviderThinkingLevel != marker {
					t.Errorf("reply marker = %q; want %q", result.ProviderThinkingLevel, marker)
				}
				select {
				case body := <-requests:
					bodies = append(bodies, body)
				case <-ctx.Done():
					t.Fatal("missing scripted SSE request")
				}
				return *result
			}
			user := goai.UserMessage("Again")
			user.Timestamp = 1
			var wantEfforts []string
			var wantInputs [][]string
			switch name {
			case "retained-level-changes":
				var messages []goai.Message
				for _, effort := range []string{"high", "high", "low", "low"} {
					messages = append(messages, user)
					messages = append(messages, run(model, messages, effort))
				}
				wantEfforts = []string{"high", "high", "high", "high"}
				wantInputs = [][]string{{"user"}, {"user", "assistant", "user"}, {
					"user", "assistant", "user", "assistant", "user", "configuration_update:low",
				}, {"user", "assistant", "user", "assistant", "user", "configuration_update:low", "assistant", "user"}}
			case "requested-model-only":
				model.ID = "gpt-6-sol"
				other := *model
				other.ID = "gpt-6-luna"
				first := run(&other, []goai.Message{user}, "low")
				history := []goai.Message{user, first, user}
				second := run(model, history, "high")
				raw, err := json.Marshal(append(history, second, user))
				if err != nil {
					t.Fatal(err)
				}
				var retained []goai.Message
				if err := json.Unmarshal(raw, &retained); err != nil {
					t.Fatal(err)
				}
				run(model, retained, "low")
				run(model, []goai.Message{user}, "low")
				wantEfforts = []string{"low", "high", "high", "low"}
			case "simple-off":
				model.ID = "gpt-6-sol"
				run(model, []goai.Message{goai.UserMessage("Say hello")}, "")
				wantEfforts, wantInputs = []string{"none"}, [][]string{{"user"}}
			}
			if len(bodies) != len(wantEfforts) || len(requests) != 0 {
				t.Fatalf("captured %d requests plus %d extra; want exactly %d", len(bodies), len(requests), len(wantEfforts))
			}
			for i, body := range bodies {
				if !reflect.DeepEqual(body["reasoning"], map[string]any{"effort": wantEfforts[i], "summary": "auto"}) {
					t.Errorf("request %d reasoning = %v; want %s auto baseline", i+1, body["reasoning"], wantEfforts[i])
				}
				if input := summarizeCodexProjection(t, body["input"]); wantInputs != nil && !reflect.DeepEqual(input, wantInputs[i]) {
					t.Errorf("request %d input = %v; want %v", i+1, input, wantInputs[i])
				}
			}
		})
	}
}
