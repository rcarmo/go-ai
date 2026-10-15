package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
	"time"
)

func TestCodemode120ExtensionModelsCannotExpandHostAuthority(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	for _, tc := range []struct {
		name            string
		host, extension bool
	}{{"opt-out", true, false}, {"no-grant", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				models := tc.extension
				extension, err := NewCodemodeExtension(engine, CodemodeExtensionOptions{Models: &models})
				if err != nil {
					t.Fatal(err)
				}
				models = !models
				if err = registry.Install(&extension); err != nil {
					t.Fatal(err)
				}
				requests := 0
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					requests++
					ch := make(chan goai.Event, 1)
					if strings.Contains(input.SystemPrompt, "declare const models") {
						t.Error("paid declarations leaked", input.SystemPrompt)
					}
					if requests == 1 {
						message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "models-call", Name: "codemode", Arguments: map[string]any{"code": `if(typeof models!=="undefined")throw Error("paid globals exposed");text("no paid authority");`}}}}
						ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
					} else {
						var last goai.Message
						for _, message := range input.Messages {
							if message.Role == goai.RoleToolResult {
								last = message
							}
						}
						if last.IsError || !strings.HasSuffix(goai.ContentText(last.Content), "Output:\nno paid authority") {
							t.Error(last)
						}
						ch <- terminal("done")
					}
					close(ch)
					return ch
				})
				options.Registry = registry
				options.PaidModels = tc.host
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
		})
	}
}
