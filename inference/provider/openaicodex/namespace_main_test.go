package openaicodex

import (
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestNamespacePresenceStreamReplayMain(t *testing.T) {
	empty, first, last := "", "first", "last"
	cases := []struct {
		name           string
		initial, final *string
		onlyDone       bool
		want           string
		present        bool
	}{
		{"absent", nil, nil, false, "", false}, {"empty", &empty, nil, false, "", true},
		{"nonempty", &first, nil, false, "first", true}, {"clear", &first, &empty, false, "", true},
		{"replace", &first, &last, false, "last", true}, {"late", nil, &last, false, "last", true},
		{"done_only_empty", nil, &empty, true, "", true},
	}
	for _, transport := range []string{"sse", "websocket"} {
		for _, custom := range []bool{false, true} {
			for _, tc := range cases {
				t.Run(fmt.Sprintf("%s/custom_%t/%s", transport, custom, tc.name), func(t *testing.T) {
					model := codexGrammarModel()
					kind, name, id := "function_call", "leaf", "fc_i"
					var tools []goai.Tool
					if custom {
						kind, name, id = "custom_tool_call", "script", "ctc_i"
						tools = []goai.Tool{codexPayloadTool("input")}
					}
					makeItem := func(namespace *string) map[string]any {
						item := map[string]any{"type": kind, "name": name, "id": id, "call_id": "c"}
						if custom {
							item["input"] = "abc"
						} else {
							item["arguments"] = "{}"
						}
						if namespace != nil {
							item["namespace"] = *namespace
						}
						return item
					}
					frame := func(kind string, item map[string]any) string {
						raw, _ := json.Marshal(map[string]any{"type": kind, "output_index": 0, "item": item})
						return string(raw)
					}
					var frames []string
					if !tc.onlyDone {
						frames = append(frames, frame("response.output_item.added", makeItem(tc.initial)))
					}
					frames = append(frames, frame("response.output_item.done", makeItem(tc.final)), `{"type":"response.completed","response":{"id":"r","status":"completed","usage":{}}}`)
					starts, ends := 0, 0
					var done *goai.Message
					check := func(namespace string, present bool) {
						if namespace != tc.want || present != tc.present {
							t.Errorf("namespace=%q present=%t want=%q/%t", namespace, present, tc.want, tc.present)
						}
					}
					receive := func(event goai.Event) {
						switch e := event.(type) {
						case *goai.ErrorEvent:
							t.Error(e.Err)
						case *goai.ToolCallStartEvent:
							starts++
							want := tc.initial
							if tc.onlyDone {
								want = tc.final
							}
							block := e.Partial.Content[e.ContentIndex]
							if (want != nil) != block.NamespacePresent || (want != nil && block.Namespace != *want) {
								t.Error("start namespace", block.Namespace, block.NamespacePresent, want)
							}
						case *goai.ToolCallEndEvent:
							ends++
							check(e.ToolCall.Namespace, e.ToolCall.NamespacePresent)
							encoded, err := json.Marshal(e.ToolCall)
							if err != nil {
								t.Error(err)
							}
							var fields map[string]any
							_ = json.Unmarshal(encoded, &fields)
							_, present := fields["namespace"]
							if present != tc.present {
								t.Error("end JSON lost namespace presence", string(encoded))
							}
						case *goai.DoneEvent:
							done = e.Message
						}
					}
					if err := streamCodexArgumentFramesWithTools(t, transport, frames, tools, receive); err != nil {
						t.Fatal(err)
					}
					if starts != 1 || ends != 1 || done == nil || len(done.Content) != 1 {
						t.Fatal("event sequence", starts, ends, done)
					}
					check(done.Content[0].Namespace, done.Content[0].NamespacePresent)
					for _, provenance := range []string{"same", "model", "provider", "api"} {
						target := *model
						switch provenance {
						case "model":
							target.ID = "other"
						case "provider":
							target.Provider = goai.ProviderOpenRouter
						case "api":
							target.Api = goai.ApiOpenAICompletions
						}
						view, err := newCodexToolView(&target, &goai.Context{Tools: tools})
						if err != nil {
							t.Fatal(err)
						}
						items := buildCodexAssistantItemsWithToolView(*done, &target, view)
						var call map[string]any
						for _, item := range items {
							candidate := item.(map[string]any)
							if candidate["type"] == kind {
								call = candidate
							}
						}
						if call == nil {
							t.Fatal("missing replay call", items)
						}
						value, present := call["namespace"]
						wantPresent := provenance == "same" && tc.present
						if present != wantPresent || (wantPresent && value != tc.want) {
							t.Fatal("namespace replay provenance", provenance, call, tc)
						}
					}
				})
			}
		}
	}
}
