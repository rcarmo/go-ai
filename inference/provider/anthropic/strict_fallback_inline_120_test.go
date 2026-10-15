package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestAnthropic120InlineStrictFallbackPreservesTranscript(t *testing.T) {
	for _, redefine := range []bool{false, true} {
		t.Run(map[bool]string{false: "addition", true: "redefinition"}[redefine], func(t *testing.T) {
			preferred := goai.Tool{Name: "preferred", Parameters: json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"},"optional":{"type":"number"}},"required":["value"]}`), ConstrainedSampling: &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "prefer"}}
			required := preferred
			required.Name = "required"
			required.ConstrainedSampling = &goai.ToolConstrainedSampling{Type: "json_schema", Strict: "require"}
			input := &goai.Context{Tools: []goai.Tool{required}, Messages: []goai.Message{{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "before"}}}, {Role: goai.RoleSystem, ToolsAdded: []goai.Tool{preferred}}, {Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "after"}}}}}
			if redefine {
				input.Tools = append(input.Tools, preferred)
			}
			before, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				foundPreferred, foundRequired := 0, 0
				var inspect func(any)
				inspect = func(value any) {
					switch node := value.(type) {
					case map[string]any:
						if name, ok := node["name"].(string); ok && node["input_schema"] != nil {
							switch name {
							case "preferred":
								foundPreferred++
								if attempts == 1 && node["strict"] != true {
									t.Error("initial preferred not strict", node)
								}
								if attempts == 2 {
									if node["strict"] != nil {
										t.Error("preferred remained strict", node)
									}
									schema := node["input_schema"].(map[string]any)
									properties := schema["properties"].(map[string]any)
									optional := properties["optional"].(map[string]any)
									if optional["type"] != "number" || optional["anyOf"] != nil {
										t.Error("original optional schema not restored", schema)
									}
								}
							case "required":
								foundRequired++
								if node["strict"] != true {
									t.Error("required downgraded", node)
								}
							}
						}
						for _, child := range node {
							inspect(child)
						}
					case []any:
						for _, child := range node {
							inspect(child)
						}
					}
				}
				inspect(body)
				if foundPreferred == 0 || foundRequired == 0 {
					t.Error("declarations missing", foundPreferred, foundRequired)
				}
				if attempts == 1 {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(400)
					_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"The compiled grammar is too large"}}`))
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"inline\",\"role\":\"assistant\",\"content\":[],\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
			}))
			defer server.Close()
			model := strictAnthropicModel()
			model.BaseURL = server.URL
			yes := true
			model.AnthropicCompat = &goai.AnthropicMessagesCompat{SupportsStrictTools: &yes, SupportsMidConvoToolChanges: &yes, SupportsMidConvoSystemMessages: &yes}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			done := false
			for event := range streamAnthropic(ctx, model, input, &goai.StreamOptions{APIKey: "test"}) {
				switch event := event.(type) {
				case *goai.DoneEvent:
					done = true
				case *goai.ErrorEvent:
					t.Error(event.Err)
				}
			}
			after, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || attempts != 2 || !done {
				t.Fatal("inline fallback failed or mutated transcript", attempts, done)
			}
		})
	}
}
