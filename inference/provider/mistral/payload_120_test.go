package mistral

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestMistral120PayloadHookSerializesSDKFields(t *testing.T) {
	observed := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		observed <- body
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
	}))
	defer server.Close()
	var original map[string]any
	opts := &goai.StreamOptions{APIKey: "fixture", OnPayload: func(payload any, _ *goai.Model) (any, error) {
		raw, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &original); err != nil {
			return nil, err
		}
		original["topP"] = 0.9
		original["randomSeed"] = 42
		original["safePrompt"] = true
		original["parallelToolCalls"] = true
		original["presencePenalty"] = 0.1
		original["frequencyPenalty"] = 0.2
		original["responseFormat"] = map[string]any{"type": "json_schema", "jsonSchema": map[string]any{"name": "result", "schemaDefinition": map[string]any{"type": "object", "properties": map[string]any{"maxTokens": map[string]any{"type": "number"}}}}}
		return original, nil
	}}
	model := &goai.Model{Provider: goai.ProviderMistral, ID: "fixture", Api: goai.ApiMistralConversations, BaseURL: server.URL}
	for event := range streamMistral(context.Background(), model, &goai.Context{}, opts) {
		if event, ok := event.(*goai.ErrorEvent); ok {
			t.Fatal(event.Err)
		}
	}
	body := <-observed
	for from, to := range map[string]string{"topP": "top_p", "randomSeed": "random_seed", "safePrompt": "safe_prompt", "parallelToolCalls": "parallel_tool_calls", "presencePenalty": "presence_penalty", "frequencyPenalty": "frequency_penalty"} {
		if _, ok := body[from]; ok {
			t.Error("SDK key leaked", from)
		}
		want := original[from]
		if to == "random_seed" {
			want = float64(42)
		}
		if !reflect.DeepEqual(body[to], want) {
			t.Error("field lost", to, body[to])
		}
	}
	format := body["response_format"].(map[string]any)
	schema := format["json_schema"].(map[string]any)["schema"].(map[string]any)
	if schema["properties"].(map[string]any)["maxTokens"] == nil {
		t.Fatal("schema property renamed", schema)
	}
	if original["responseFormat"] == nil || original["topP"] == nil {
		t.Fatal("caller payload mutated")
	}
}
