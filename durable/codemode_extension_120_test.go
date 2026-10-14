package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"sync/atomic"
	"testing"
)

func TestCodemode120ModelToolExecutesRealVMAndNestedTool(t *testing.T) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		var effects, requests atomic.Int64
		leaf := wrapRegistration("codemode_leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "leaf"}, nil
		}
		if err := registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		extension, err := NewCodemodeExtension(sandbox)
		if err != nil {
			t.Fatal(err)
		}
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				found := false
				for _, tool := range input.Tools {
					if tool.Name == "codemode" {
						found = true
						if tool.ConstrainedSampling == nil || tool.ConstrainedSampling.Variants["openai_lark"] != CodemodeSourceGrammar {
							t.Error("grammar absent")
						}
					}
				}
				if !found {
					t.Error("codemode not offered")
				}
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "script-call", Name: "codemode", Arguments: map[string]any{"code": "text(await tools.codemode_leaf({}));"}}}}
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
			} else {
				last := input.Messages[len(input.Messages)-1]
				if last.Role != goai.RoleToolResult || goai.ContentText(last.Content) != "leaf" {
					t.Error("script transcript", last)
				}
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		sub, err := conv.Submit(bg, Input{Content: "run script"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 3e9)
		defer cancel()
		result, err := sub.Wait(ctx)
		if err != nil || result.Submission.Status != "done" || requests.Load() != 2 || effects.Load() != 1 {
			t.Fatal(result, err, requests.Load(), effects.Load())
		}
	})
}
