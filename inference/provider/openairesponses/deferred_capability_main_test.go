package openairesponses

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestResponsesLegacyDeferredCapabilityWireMain(t *testing.T) {
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
					conv := deferredContext([]goai.Tool{deferredTool("base_tool"), deferredTool("late_tool")}, []string{"late_tool"})
					before, _ := json.Marshal(conv)
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					done, hooks := 0, 0
					for event := range streamResponses(ctx, model, conv, &goai.StreamOptions{APIKey: "test", OnPayload: func(body any, _ *goai.Model) (any, error) { hooks++; return body, nil }}) {
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
						mode = "additional"
					} else if search {
						mode = "search"
					}
					tools := body["tools"].([]any)
					wantTools := 1
					if mode == "none" {
						wantTools = 2
					}
					if len(tools) != wantTools || tools[0].(map[string]any)["name"] != "base_tool" {
						t.Fatal("request tools do not match capability plan", mode, tools)
					}
					additionalCount, calls, outputs := 0, 0, 0
					marker, load, lastUser := -1, -1, -1
					for index, raw := range body["input"].([]any) {
						item := raw.(map[string]any)
						if item["role"] == "user" {
							lastUser = index
						}
						if item["type"] == "function_call_output" {
							marker = index
						}
						switch item["type"] {
						case "additional_tools":
							additionalCount++
							load = index
							declared := item["tools"].([]any)
							if len(declared) != 1 || declared[0].(map[string]any)["name"] != "late_tool" {
								t.Error("initial header advertised deferred definitions", item)
							}
						case "tool_search_call":
							calls++
							load = index
							id := "pi_tool_load_f88061"
							if mid {
								id = "pi_tool_load_c1765fa1"
							}
							if item["call_id"] != id || !reflect.DeepEqual(item["arguments"], map[string]any{"query": "late_tool", "limit": float64(1)}) {
								t.Error("legacy identity/required arguments changed", item)
							}
						case "tool_search_output":
							outputs++
							declared := item["tools"].([]any)
							if len(declared) != 1 || declared[0].(map[string]any)["name"] != "late_tool" || declared[0].(map[string]any)["defer_loading"] != true {
								t.Error("bad deferred definition", item)
							}
						}
					}
					switch mode {
					case "none":
						if additionalCount+calls+outputs != 0 {
							t.Fatal("unsupported mode emitted loads", body)
						}
					case "additional":
						if additionalCount != 1 || calls+outputs != 0 {
							t.Fatal("additional mode duplicated loads", body)
						}
					case "search":
						if additionalCount != 0 || calls != 1 || outputs != 1 {
							t.Fatal("search fallback duplicated loads", body)
						}
					}
					if mode != "none" && (marker < 0 || load <= marker || lastUser <= load) {
						t.Fatal("legacy tool declaration moved before its marker", marker, load, lastUser, body)
					}
					after, _ := json.Marshal(conv)
					if string(before) != string(after) {
						t.Fatal("planning mutated caller context")
					}
				})
			}
		}
	}
}

func TestResponsesLegacyAllMarkedFallbackDoesNotDuplicateHeaderMain(t *testing.T) {
	enabled := true
	model := responsesDeferredModel("gpt-5.4", goai.ProviderOpenAI)
	model.ResponsesCompat = &goai.OpenAIResponsesCompat{SupportsMidConvoSystemMessages: &enabled, SupportsAdditionalTools: &enabled}
	conv := deferredContext([]goai.Tool{deferredTool("late_tool")}, []string{"late_tool"})
	request := buildRequest(model, conv, &goai.StreamOptions{})
	if len(request.Tools) != 1 || request.Tools[0].Name != "late_tool" {
		t.Fatal("fallback lost sole tool", request.Tools)
	}
	var input []map[string]any
	if err := json.Unmarshal(request.Input, &input); err != nil {
		t.Fatal(err)
	}
	for _, item := range input {
		if item["type"] == "additional_tools" || item["type"] == "tool_search_output" {
			t.Fatal("fallback snapshot re-announced by initial header", input)
		}
	}
}
