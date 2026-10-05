package tools

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"strings"
	"sync/atomic"
	"testing"
)

func TestEditArgumentPreparationRepairsLegacyShapesWithoutMutatingProvenance(t *testing.T) {
	for _, mode := range []string{"legacy", "object", "json", "combined-array", "combined-object", "combined-json"} {
		t.Run(mode, func(t *testing.T) {
			registry := durable.NewRegistry()
			effects := atomic.Int64{}
			registration := Edit(LocalEnv(t.TempDir()))
			registration.Execute = func(_ context.Context, args durable.JSON, _ *durable.ToolAPI) (durable.ToolResult, error) {
				effects.Add(1)
				edits, ok := args["edits"].([]any)
				want := 1
				if strings.HasPrefix(mode, "combined-") {
					want = 2
				}
				if !ok || len(edits) != want {
					t.Error("repair lost edits", args)
				}
				return durable.ToolResult{Content: "edited"}, nil
			}
			if err := registry.Register(registration); err != nil {
				t.Fatal(err)
			}
			args := map[string]any{"path": "file"}
			switch mode {
			case "legacy":
				args["oldText"], args["newText"] = "old", "new"
			case "object":
				args["edits"] = map[string]any{"oldText": "old", "newText": "new"}
			case "json":
				args["edits"] = `[{"oldText":"old","newText":"new"}]`
			case "combined-array":
				args["edits"] = []any{map[string]any{"oldText": "a", "newText": "b"}}
			case "combined-object":
				args["edits"] = map[string]any{"oldText": "a", "newText": "b"}
			case "combined-json":
				args["edits"] = `[{"oldText":"a","newText":"b"}]`
			}
			if strings.HasPrefix(mode, "combined-") {
				args["oldText"], args["newText"] = "old", "new"
			}
			api := goai.Api("edit-prepare-" + mode)
			calls := atomic.Int64{}
			goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}
				if calls.Add(1) == 1 {
					message.StopReason = goai.StopReasonToolUse
					message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "edit-call", Name: "edit", Arguments: args}}
				}
				ch := make(chan goai.Event, 1)
				ch <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
				close(ch)
				return ch
			}})
			defer goai.UnregisterApi(api)
			store, _ := durable.NewMemory()
			h, err := durable.Open(context.Background(), store, durable.Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model {
				return &goai.Model{ID: "edit-model", Provider: goai.ProviderOpenAI, Api: api, ContextWindow: 4096, MaxTokens: 100}
			}})
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close(context.Background())
			conversation, err := h.Root(context.Background(), durable.AgentChange{Model: durable.ModelRef{Provider: goai.ProviderOpenAI, ID: "edit-model"}})
			if err != nil {
				t.Fatal(err)
			}
			sub, err := conversation.Submit(context.Background(), durable.Input{Content: "edit"})
			if err != nil {
				t.Fatal(err)
			}
			settled, err := sub.Wait(context.Background())
			if err != nil || settled.Submission.Status != "done" || effects.Load() != 1 {
				t.Fatal(settled, effects.Load(), err)
			}
			view, err := conversation.ContextView(context.Background(), 0)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, message := range view.Messages {
				for _, block := range message.Content {
					if block.Type == "toolCall" {
						found = true
						switch mode {
						case "legacy":
							if block.Arguments["oldText"] != "old" || block.Arguments["edits"] != nil {
								t.Error("model provenance repaired", block)
							}
						case "json":
							if _, ok := block.Arguments["edits"].(string); !ok {
								t.Error("model provenance parsed")
							}
						}
					}
				}
			}
			if !found {
				t.Fatal("call provenance missing")
			}
			if mode == "json" && !strings.HasPrefix(args["edits"].(string), "[") {
				t.Fatal("caller args mutated")
			}
		})
	}
}

func TestEditNormalizesLineEndingsPreservesBOMAndRejectsNoOp(t *testing.T) {
	input := "\ufefffirst\r\nsecond\r\nthird\r\n"
	output, details, err := applyEdits(input, []replaceEdit{{OldText: "first\nsecond", NewText: "changed\nline"}}, "file")
	if err != nil || output != "\ufeffchanged\r\nline\r\nthird\r\n" || details["firstChangedLine"] != 1 {
		t.Fatal(output, details, err)
	}
	if _, _, err := applyEdits(input, []replaceEdit{{OldText: "first", NewText: "first"}}, "file"); err == nil {
		t.Fatal("no-op edit accepted")
	}
	if _, _, err := applyEdits("a\r\nb\r\na\nb", []replaceEdit{{OldText: "a\nb", NewText: "x"}}, "file"); err == nil {
		t.Fatal("normalized duplicate accepted")
	}
}
