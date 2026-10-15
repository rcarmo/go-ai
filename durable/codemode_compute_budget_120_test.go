package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
	"time"
)

func TestCodemode120ModelComputeBudgetCaptured(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		budget := 10
		extension, err := NewCodemodeExtension(engine, CodemodeExtensionOptions{InterruptBudget: &budget})
		if err != nil {
			t.Fatal(err)
		}
		budget = 1000
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		requests := 0
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			requests++
			ch := make(chan goai.Event, 1)
			if requests == 1 {
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "budget-call", Name: "codemode", Arguments: map[string]any{"code": `for(;;){}`}}}}
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
			} else {
				var last goai.Message
				for _, message := range input.Messages {
					if message.Role == goai.RoleToolResult {
						last = message
					}
				}
				if !last.IsError || !strings.Contains(goai.ContentText(last.Content), "interrupt budget of 10 polls") {
					t.Error("budget failure transcript", last)
				}
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		sub, err := conv.Submit(bg, Input{Content: "compute"})
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
	for _, budget := range []int{-1, 1000001} {
		if _, err = NewCodemodeExtension(engine, CodemodeExtensionOptions{InterruptBudget: &budget}); err == nil {
			t.Fatal("invalid budget accepted", budget)
		}
	}
}
