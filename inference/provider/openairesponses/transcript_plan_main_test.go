package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/internal/responsestream"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func responsesAnchorFixture() *goai.Context {
	old, newValue := "old", "new"
	revised := deferredTool("a")
	revised.Description = "revised"
	return &goai.Context{Messages: []goai.Message{
		{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "initial"}}, Sections: map[string]*string{"scope": &old}, ToolsAdded: []goai.Tool{deferredTool("a"), deferredTool("b")}},
		goai.UserMessage("hello"),
		{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "later"}}, Sections: map[string]*string{"scope": &newValue}, ToolsRemoved: []goai.ToolReference{{Name: "b"}}},
		{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{deferredTool("c")}},
		{Role: goai.RoleSystem, ToolsRemoved: []goai.ToolReference{{Name: "a"}}, ToolsAdded: []goai.Tool{revised}},
		{Role: goai.RoleSystem, Sections: map[string]*string{"scope": nil}},
		{Role: goai.RoleSystem, ToolsRemoved: []goai.ToolReference{{Name: "a"}}},
		{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{deferredTool("d")}},
		goai.UserMessage("end"),
	}}
}

func TestResponsesTranscriptAnchorsWireMain(t *testing.T) {
	for _, mid := range []bool{false, true} {
		for _, additional := range []bool{false, true} {
			for _, search := range []bool{false, true} {
				t.Run(fmt.Sprintf("mid_%t/additional_%t/search_%t", mid, additional, search), func(t *testing.T) {
					captured := make(chan map[string]any, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var body map[string]any
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
							return
						}
						captured <- body
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[]}}\n\n")
					}))
					defer server.Close()
					model := responsesDeferredModel("gpt-5.4", goai.ProviderOpenAI)
					model.BaseURL = server.URL
					model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsMidConvoSystemMessages: &mid, SupportsAdditionalTools: &additional, SupportsToolSearch: &search}
					conv := responsesAnchorFixture()
					before, _ := json.Marshal(conv)
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					hooks, done := 0, 0
					for event := range streamResponses(ctx, model, conv, &goai.StreamOptions{APIKey: "test", OnPayload: func(payload any, _ *goai.Model) (any, error) { hooks++; return payload, nil }}) {
						switch e := event.(type) {
						case *goai.ErrorEvent:
							t.Error(e.Err)
						case *goai.DoneEvent:
							done++
						}
					}
					if done != 1 || hooks != 1 {
						t.Fatal("completion/hook mismatch", done, hooks)
					}
					var body map[string]any
					select {
					case body = <-captured:
					case <-ctx.Done():
						t.Fatal("missing request")
					}
					mode := "none"
					if mid && additional {
						mode = "changes"
					} else if mid && search {
						mode = "additions"
					}
					var requestNames []string
					if raw, ok := body["tools"].([]any); ok {
						for _, item := range raw {
							requestNames = append(requestNames, item.(map[string]any)["name"].(string))
						}
					}
					wantNames := []string{"c", "d"}
					if mode == "changes" {
						wantNames = nil
					} else if mode == "additions" {
						wantNames = []string{"c"}
					}
					if !reflect.DeepEqual(requestNames, wantNames) {
						t.Fatal("request prefix", mode, requestNames, wantNames)
					}
					var groups []string
					var text []string
					calls, outputs := 0, 0
					for _, raw := range body["input"].([]any) {
						item := raw.(map[string]any)
						if content, ok := item["content"].(string); ok {
							text = append(text, content)
						}
						switch item["type"] {
						case "additional_tools":
							var names []string
							for _, tool := range item["tools"].([]any) {
								definition := tool.(map[string]any)
								names = append(names, definition["name"].(string))
								if definition["defer_loading"] != nil {
									t.Error("changes definitions must not defer", item)
								}
							}
							groups = append(groups, strings.Join(names, ","))
						case "tool_search_call":
							calls++
							if item["call_id"] != responsestream.ToolLoadID("system:6", []string{"d"}) || !reflect.DeepEqual(item["arguments"], map[string]any{"query": "d", "limit": float64(1)}) {
								t.Error("wrong load identity/arguments", item)
							}
						case "tool_search_output":
							outputs++
							tools := item["tools"].([]any)
							if item["call_id"] != responsestream.ToolLoadID("system:6", []string{"d"}) || len(tools) != 1 || tools[0].(map[string]any)["name"] != "d" || tools[0].(map[string]any)["defer_loading"] != true {
								t.Error("wrong deferred output", item)
							}
						}
					}
					switch mode {
					case "changes":
						if !reflect.DeepEqual(groups, []string{"a,b", "c", "a", "d"}) || calls+outputs != 0 {
							t.Fatal("change anchors", groups, calls, outputs)
						}
						if !strings.Contains(strings.Join(text, "\n"), "unavailable") {
							t.Fatal("missing removal notice:", text)
						}
					case "additions":
						if len(groups) != 0 || calls != 1 || outputs != 1 {
							t.Fatal("folded search anchors", groups, calls, outputs)
						}
					case "none":
						if len(groups)+calls+outputs != 0 {
							t.Fatal("snapshot emitted anchors", body)
						}
					}
					allText := strings.Join(text, "\n")
					if !mid {
						if !reflect.DeepEqual(text, []string{"initial\n\nlater"}) {
							t.Fatal("collapsed system projection", text)
						}
					} else if !strings.Contains(allText, "initial\n\nold") || !strings.Contains(allText, "Updated system prompt section \"scope\":\n\nnew") || !strings.Contains(allText, "Removed system prompt section \"scope\".") {
						t.Fatal("system section projection", text)
					}
					after, _ := json.Marshal(conv)
					if string(before) != string(after) {
						t.Fatal("mutated caller context")
					}
				})
			}
		}
	}
}

func TestResponsesTranscriptWireBudgetAndNoMutationMain(t *testing.T) {
	mid, additional := true, true
	model := responsesDeferredModel("gpt-5.4", goai.ProviderOpenAI)
	model.ContextWindow = 50000
	model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsMidConvoSystemMessages: &mid, SupportsAdditionalTools: &additional}
	conv := responsesAnchorFixture()
	conv.Messages[3].ToolsAdded[0].Description = strings.Repeat("large schema content ", 1000)
	before, _ := json.Marshal(conv)
	max := 40000
	opts := &goai.StreamOptions{MaxTokens: &max}
	request := buildRequest(model, conv, opts)
	budget := &goai.Context{Messages: []goai.Message{goai.UserMessage(string(request.Input))}}
	expected := goai.ClampStreamMaxTokensPtr(model, budget, opts)
	if request.MaxOutputTokens == nil || expected == nil || *request.MaxOutputTokens != *expected {
		t.Fatal("anchored declarations missing from wire budget", request.MaxOutputTokens, expected)
	}
	after, _ := json.Marshal(conv)
	if string(before) != string(after) || *opts.MaxTokens != 40000 {
		t.Fatal("budget mutated caller inputs")
	}
}

func TestResponsesTranscriptInvalidAnchoredSchemaFailsBeforeSendMain(t *testing.T) {
	enabled := true
	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests <- struct{}{}; w.WriteHeader(500) }))
	defer server.Close()
	model := responsesDeferredModel("gpt-5.4", goai.ProviderOpenAI)
	model.BaseURL = server.URL
	model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsMidConvoSystemMessages: &enabled, SupportsAdditionalTools: &enabled}
	bad := deferredTool("broken")
	bad.Parameters = json.RawMessage(`{`)
	conv := &goai.Context{Messages: []goai.Message{{Role: goai.RoleSystem, ToolsAdded: []goai.Tool{bad}}, goai.UserMessage("hello")}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	failures, hooks := 0, 0
	for event := range streamResponses(ctx, model, conv, &goai.StreamOptions{APIKey: "test", OnPayload: func(payload any, _ *goai.Model) (any, error) { hooks++; return payload, nil }}) {
		if _, ok := event.(*goai.ErrorEvent); ok {
			failures++
		}
	}
	if failures != 1 || hooks != 0 {
		t.Fatal("invalid anchored schema reached payload hook", failures, hooks)
	}
	select {
	case <-requests:
		t.Fatal("invalid schema sent to provider")
	default:
	}
}
