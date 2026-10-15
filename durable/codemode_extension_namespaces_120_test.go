package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
	"time"
)

func TestCodemode120ExtensionNamespacesCapturedForPromptAndVM(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		if err = registry.Register(wrapRegistration("ns_leaf")); err != nil {
			t.Fatal(err)
		}
		namespaces := []CodemodeNamespace{{Name: "extension", Prefix: "ns_", Description: "original"}}
		extension, err := NewCodemodeExtension(engine, CodemodeExtensionOptions{Namespaces: &namespaces})
		if err != nil {
			t.Fatal(err)
		}
		namespaces[0].Description = "mutated"
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		requests := 0
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			requests++
			ch := make(chan goai.Event, 1)
			if !strings.Contains(input.SystemPrompt, "## extension\noriginal") || strings.Contains(input.SystemPrompt, "## harness") {
				t.Error("namespace prompt drift", input.SystemPrompt)
			}
			if requests == 1 {
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "ns-call", Name: "codemode", Arguments: map[string]any{"code": `const ns=await describeNamespace("extension");if(ns.description!=="original")throw Error("namespace drift");text(await tools.ns_leaf({}));`}}}}
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
			} else {
				last := input.Messages[len(input.Messages)-1]
				if last.IsError || !strings.HasSuffix(goai.ContentText(last.Content), "Output:\nbase") {
					t.Error(last)
				}
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		options.CodemodeNamespaces = []CodemodeNamespace{{Name: "harness", Prefix: "ns_", Description: "wrong"}}
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
	invalid := []CodemodeNamespace{{Name: "bad", Prefix: ""}}
	if _, err = NewCodemodeExtension(engine, CodemodeExtensionOptions{Namespaces: &invalid}); err == nil {
		t.Fatal("invalid namespace accepted")
	}
}
