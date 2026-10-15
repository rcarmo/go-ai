package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
	"time"
)

func TestCodemode120ExtensionInlineBudgetCapturedAndZeroDiscovery(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	for _, deferred := range []bool{false, true} {
		name := "inline"
		if deferred {
			name = "deferred"
		}
		t.Run(name, func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				registry := NewRegistry()
				leaf := wrapRegistration("budget_leaf")
				leaf.Deferred = deferred
				if err = registry.Register(leaf); err != nil {
					t.Fatal(err)
				}
				budget := 0
				extension, err := NewCodemodeExtension(engine, CodemodeExtensionOptions{InlineBudget: &budget})
				if err != nil {
					t.Fatal(err)
				}
				budget = 3000
				if err = registry.Install(&extension); err != nil {
					t.Fatal(err)
				}
				requests := 0
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					requests++
					ch := make(chan goai.Event, 1)
					for _, tool := range input.Tools {
						if deferred && tool.Name == "budget_leaf" {
							t.Error("deferred tool offered directly to model")
						}
					}
					if strings.Contains(input.SystemPrompt, "### `budget_leaf`") || !deferred && !strings.Contains(input.SystemPrompt, "Some tools are not listed") {
						t.Error("zero budget not captured", input.SystemPrompt)
					}
					if requests == 1 {
						message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "budget-call", Name: "codemode", Arguments: map[string]any{"code": `const hits=await searchTools("budget_leaf");if(hits[0].name!=="budget_leaf")throw Error("not discoverable");const description=await describeTool("budget_leaf");if(!description.includes("budget_leaf"))throw Error("not describable");text(await tools.budget_leaf({}));`}}}}
						ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
					} else {
						last := input.Messages[len(input.Messages)-1]
						if !strings.HasPrefix(goai.ContentText(last.Content), "Script completed\nWall time ") || !strings.HasSuffix(goai.ContentText(last.Content), "Output:\nbase") {
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
				sub, err := conv.Submit(bg, Input{Content: "discover"})
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
	negative := -1
	if _, err = NewCodemodeExtension(engine, CodemodeExtensionOptions{InlineBudget: &negative}); err == nil {
		t.Fatal("negative budget accepted")
	}
	if _, err = NewCodemodeExtension(engine, CodemodeExtensionOptions{}, CodemodeExtensionOptions{}); err == nil {
		t.Fatal("multiple option sets accepted")
	}
}
