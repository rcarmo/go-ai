package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
	"time"
)

func TestCodemode120ExtensionGlobalsCapturedAndExecuted(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		schema := JSON{"type": "string"}
		globals := []CodemodeGlobal{{Name: "host.lookup", Version: 1, InputSchema: schema, OutputSchema: JSON{"type": "string"}, Execute: func(context.Context, any) (any, error) { return "captured", nil }}}
		extension, err := NewCodemodeExtension(engine, CodemodeExtensionOptions{Globals: &globals})
		if err != nil {
			t.Fatal(err)
		}
		globals[0].Execute = func(context.Context, any) (any, error) { return "mutated", nil }
		schema["type"] = "number"
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		requests := 0
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			requests++
			ch := make(chan goai.Event, 1)
			if !strings.Contains(input.SystemPrompt, "lookup(args: string): Promise<string>") {
				t.Error("global metadata drift", input.SystemPrompt)
			}
			if requests == 1 {
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "global-call", Name: "codemode", Arguments: map[string]any{"code": `text(await host.lookup("key"));`}}}}
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
			} else {
				var last goai.Message
				for _, message := range input.Messages {
					if message.Role == goai.RoleToolResult {
						last = message
					}
				}
				if last.IsError || !strings.HasSuffix(goai.ContentText(last.Content), "Output:\ncaptured") {
					t.Error(last)
				}
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		sub, err := conv.Submit(bg, Input{Content: "run"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 5*time.Second)
		defer cancel()
		result, err := sub.Wait(ctx)
		if err != nil || result.Submission.Status != "done" || requests != 2 {
			t.Fatal(result, err, requests)
		}
	})
}
