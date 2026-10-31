package bedrock

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime/types"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

func mainStrictTool(name, mode string) goai.Tool {
	return goai.Tool{Name: name, Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}}}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: mode}}
}

func TestBedrockPreferredStrictFallbackWireMain(t *testing.T) {
	for _, mode := range []string{"success", "grammar", "tool-count", "union-count", "unrelated", "wrong-type", "required-only", "unsupported", "hook-error", "cancel", "none", "require-unsupported"} {
		t.Run(mode, func(t *testing.T) {
			bodies := make(chan map[string]any, 3)
			var calls atomic.Int32
			message := "The compiled grammar is too large"
			if mode == "tool-count" {
				message = "Too many strict tools (21), the limit is 20"
			}
			if mode == "union-count" {
				message = "Schemas contains too many parameters with union types (17)"
			}
			if mode == "unrelated" {
				message = "unrelated validation error"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				bodies <- body
				if calls.Add(1) == 2 && mode == "success" {
					w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
					encoder := eventstream.NewEncoder()
					for _, frame := range []struct{ name, payload string }{{"messageStart", `{"role":"assistant"}`}, {"messageStop", `{"stopReason":"end_turn"}`}, {"metadata", `{"usage":{"inputTokens":0,"outputTokens":0,"totalTokens":0},"metrics":{"latencyMs":0}}`}} {
						var headers eventstream.Headers
						headers.Set(":message-type", eventstream.StringValue("event"))
						headers.Set(":event-type", eventstream.StringValue(frame.name))
						headers.Set(":content-type", eventstream.StringValue("application/json"))
						if err := encoder.Encode(w, eventstream.Message{Headers: headers, Payload: []byte(frame.payload)}); err != nil {
							t.Error(err)
						}
					}
					return
				}
				w.Header().Set("Content-Type", "application/json")
				kind, status := "ValidationException", 400
				if mode == "wrong-type" {
					kind, status = "AccessDeniedException", 403
				}
				w.Header().Set("X-Amzn-Errortype", kind)
				w.WriteHeader(status)
				json.NewEncoder(w).Encode(map[string]any{"message": message})
			}))
			defer server.Close()
			supported := mode != "unsupported" && mode != "require-unsupported"
			model := &goai.Model{ID: "test-model", Provider: goai.ProviderAmazonBedrock, Api: goai.ApiBedrockConverseStream, BaseURL: server.URL, BedrockCompat: &goai.BedrockCompat{SupportsStrictMode: &supported}}
			conv := &goai.Context{Messages: []goai.Message{goai.UserMessage("hi")}, Tools: []goai.Tool{mainStrictTool("preferred", "prefer"), mainStrictTool("required", "require"), {Name: "ordinary", Parameters: mainStrictTool("ordinary", "").Parameters}}}
			if mode == "required-only" || mode == "require-unsupported" {
				conv.Tools = conv.Tools[1:]
			}
			if mode == "unsupported" {
				conv.Tools = conv.Tools[:1]
			}
			before, _ := json.Marshal(conv)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			hooks := 0
			opts := &goai.StreamOptions{Region: "us-east-1", ToolChoice: goai.ToolChoiceRequired, Env: goai.ProviderEnv{"AWS_BEDROCK_SKIP_AUTH": "1", "AWS_EC2_METADATA_DISABLED": "true"}, OnPayload: func(payload any, _ *goai.Model) (any, error) {
				hooks++
				input := payload.(*bedrockruntime.ConverseStreamInput)
				if hooks == 1 {
					if input.InferenceConfig == nil {
						input.InferenceConfig = &types.InferenceConfiguration{}
					}
					input.InferenceConfig.MaxTokens = aws.Int32(29)
					input.RequestMetadata = map[string]string{"caller": "preserved"}
				}
				if hooks == 2 && mode == "hook-error" {
					return nil, errors.New("hook refused retry")
				}
				if hooks == 2 && mode == "cancel" {
					cancel()
				}
				return input, nil
			}}
			if mode == "none" {
				opts.ToolChoice = goai.ToolChoiceNone
			}
			var terminal *goai.ErrorEvent
			var completion *goai.DoneEvent
			for event := range streamBedrock(ctx, model, conv, opts) {
				if e, ok := event.(*goai.DoneEvent); ok {
					completion = e
				}
				if e, ok := event.(*goai.ErrorEvent); ok {
					if terminal != nil {
						t.Fatal("multiple terminal errors")
					}
					terminal = e
				}
			}
			if mode == "success" {
				if completion == nil || terminal != nil {
					t.Fatal("fallback did not complete", terminal)
				}
			} else if terminal == nil {
				t.Fatal("missing terminal error")
			}
			fallback := mode == "success" || mode == "grammar" || mode == "tool-count" || mode == "union-count" || mode == "hook-error" || mode == "cancel"
			want := 1
			if fallback {
				want = 2
			}
			if mode == "hook-error" || mode == "cancel" {
				want = 1
			}
			if mode == "require-unsupported" {
				want = 0
			}
			close(bodies)
			var requests []map[string]any
			for body := range bodies {
				requests = append(requests, body)
			}
			if len(requests) != want {
				t.Fatalf("requests %d, want %d", len(requests), want)
			}
			wantHooks := want
			if fallback {
				wantHooks = 2
			}
			if hooks != wantHooks {
				t.Fatal("payload hook count", hooks, wantHooks)
			}
			for i, body := range requests {
				if mode == "none" {
					if _, ok := body["toolConfig"]; ok {
						t.Fatal("none still declares tools", body)
					}
					continue
				}
				config := body["toolConfig"].(map[string]any)
				if !reflect.DeepEqual(config["toolChoice"], map[string]any{"any": map[string]any{}}) {
					t.Fatal("required tool choice projection", config)
				}
				for _, raw := range config["tools"].([]any) {
					spec := raw.(map[string]any)["toolSpec"].(map[string]any)
					name := spec["name"]
					strict, present := spec["strict"]
					expect := supported && name != "ordinary" && !(i == 1 && name == "preferred")
					if expect {
						if !present || strict != true {
							t.Fatal("strict flag lost", spec)
						}
					} else if present {
						t.Fatal("strict flag must be omitted", spec)
					}
					schema := spec["inputSchema"].(map[string]any)["json"].(map[string]any)
					if !expect {
						var original map[string]any
						json.Unmarshal(mainStrictTool("original", "").Parameters, &original)
						if !reflect.DeepEqual(schema, original) {
							t.Fatal("non-strict schema changed", schema)
						}
					}
					if expect && schema["additionalProperties"] != false {
						t.Fatal("wire strict schema not converted", schema)
					}
				}
				if !reflect.DeepEqual(body["requestMetadata"], map[string]any{"caller": "preserved"}) {
					t.Fatal("metadata edit lost", body)
				}
				if body["inferenceConfig"].(map[string]any)["maxTokens"] != float64(29) {
					t.Fatal("unrelated payload edit lost", body)
				}
			}
			diagnostics := 0
			var final *goai.Message
			if completion != nil {
				final = completion.Message
			} else if terminal != nil {
				final = terminal.Error
			}
			if final != nil {
				for _, d := range final.Diagnostics {
					if d.Type == "bedrock_strict_tools_fallback" {
						diagnostics++
						if d.Details["reason"] != "strict_tool_limits_exceeded" || d.Timestamp == 0 {
							t.Fatal("fallback diagnostic", d)
						}
					}
				}
			}
			wantDiag := 0
			if fallback {
				wantDiag = 1
			}
			if diagnostics != wantDiag {
				t.Fatal("diagnostic count", diagnostics, wantDiag)
			}
			if mode == "cancel" && terminal.Reason != goai.StopReasonAborted {
				t.Fatal("cancellation lost", terminal)
			}
			after, _ := json.Marshal(conv)
			if string(before) != string(after) {
				t.Fatal("caller context mutated")
			}
		})
	}
}

func TestBedrockStrictToolConfigProjectionMain(t *testing.T) {
	supported := true
	model := &goai.Model{ID: "test", BedrockCompat: &goai.BedrockCompat{SupportsStrictMode: &supported}}
	conv := &goai.Context{Tools: []goai.Tool{mainStrictTool("preferred", "prefer")}}
	for _, choice := range []goai.ToolChoice{goai.ToolChoiceAuto, goai.ToolChoiceRequired, goai.ToolChoiceNone} {
		t.Run(fmt.Sprint(choice), func(t *testing.T) {
			got := buildConverseInput(model, conv, &goai.StreamOptions{ToolChoice: choice})
			if choice == goai.ToolChoiceNone {
				if got.ToolConfig != nil {
					t.Fatal("tools not suppressed")
				}
				return
			}
			if choice == goai.ToolChoiceAuto {
				if _, ok := got.ToolConfig.ToolChoice.(*types.ToolChoiceMemberAuto); !ok {
					t.Fatal("auto choice")
				}
			}
			spec := got.ToolConfig.Tools[0].(*types.ToolMemberToolSpec).Value
			if spec.Strict == nil || !*spec.Strict {
				t.Fatal("strict flag missing")
			}
			raw, err := spec.InputSchema.(*types.ToolInputSchemaMemberJson).Value.MarshalSmithyDocument()
			if err != nil {
				t.Fatal(err)
			}
			var schema map[string]any
			json.Unmarshal(raw, &schema)
			if schema["additionalProperties"] != false {
				t.Fatal("emitted strict schema", schema)
			}
		})
	}
}

func TestBedrockStrictFallbackDiagnosticSurvivesStreamMain(t *testing.T) {
	events := make(chan types.ConverseStreamOutput, 2)
	events <- &types.ConverseStreamOutputMemberMessageStart{Value: types.MessageStartEvent{Role: types.ConversationRoleAssistant}}
	events <- &types.ConverseStreamOutputMemberMessageStop{Value: types.MessageStopEvent{StopReason: types.StopReasonEndTurn}}
	close(events)
	reader := &fakeConverseReader{events: events}
	stream := bedrockruntime.NewConverseStreamEventStream(func(s *bedrockruntime.ConverseStreamEventStream) { s.Reader = reader })
	response := &bedrockruntime.ConverseStreamOutput{}
	setConverseEventStream(response, stream)
	diagnostic := goai.AssistantMessageDiagnostic{Type: "bedrock_strict_tools_fallback", Timestamp: 1, Details: map[string]any{"reason": "strict_tool_limits_exceeded"}}
	done := 0
	processConverseStream(response, &goai.Model{ID: "test", Api: goai.ApiBedrockConverseStream}, func(event goai.Event) {
		if e, ok := event.(*goai.DoneEvent); ok {
			done++
			if !reflect.DeepEqual(e.Message.Diagnostics, []goai.AssistantMessageDiagnostic{diagnostic}) {
				t.Fatal("stream lost fallback diagnostic", e.Message.Diagnostics)
			}
		}
	}, diagnostic)
	if done != 1 {
		t.Fatal("missing completion", done)
	}
}
