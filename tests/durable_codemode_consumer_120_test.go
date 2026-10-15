package goai_test

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This external package exercises only exported API and requires no credentials.
func TestDurable120PublicCodemodeConsumer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	sandbox, err := durable.NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(context.Background())
	registry := durable.NewRegistry()
	var effects, requests atomic.Int64
	if err = registry.Register(durable.ToolRegistration{Definition: goai.Tool{Name: "echo", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}, Implementation: "consumer.echo", Version: 1, ReplaySafe: true, Execute: func(context.Context, durable.JSON, *durable.ToolAPI) (durable.ToolResult, error) {
		effects.Add(1)
		return durable.ToolResult{Content: "public API"}, nil
	}}); err != nil {
		t.Fatal(err)
	}
	namespaces := []durable.CodemodeNamespace{{Name: "consumer", Prefix: "echo", Description: "consumer namespace"}}
	budget := 1000
	paid := false
	extension, err := durable.NewCodemodeExtension(sandbox, durable.CodemodeExtensionOptions{Namespaces: &namespaces, InterruptBudget: &budget, Models: &paid})
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.Install(&extension); err != nil {
		t.Fatal(err)
	}
	api := goai.Api("public-codemode-consumer-120")
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "public-call", Name: "codemode", Arguments: map[string]any{"code": `text(await tools.echo({}));`}}}}
			ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
		} else {
			found := false
			for _, message := range input.Messages {
				if message.Role == goai.RoleToolResult && message.ToolName == "codemode" {
					found = true
					if message.IsError || !strings.Contains(goai.ContentText(message.Content), "Output:\npublic API") {
						t.Error(message)
					}
				}
			}
			if !found {
				t.Error("tool transcript absent")
			}
			ch <- &goai.DoneEvent{Reason: goai.StopReasonStop, Message: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}}
		}
		close(ch)
		return ch
	}})
	defer goai.UnregisterApi(api)
	storage, err := durable.NewMemory()
	if err != nil {
		t.Fatal(err)
	}
	model := &goai.Model{ID: "consumer", Provider: goai.ProviderOpenAI, Api: api, ContextWindow: 128000, MaxTokens: 256}
	harness, err := durable.Open(ctx, storage, durable.Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }})
	if err != nil {
		t.Fatal(err)
	}
	defer harness.Close(context.Background())
	conversation, err := harness.Root(ctx, durable.AgentChange{Model: durable.ModelRef{Provider: model.Provider, ID: model.ID}})
	if err != nil {
		t.Fatal(err)
	}
	submission, err := conversation.Submit(ctx, durable.Input{Content: "run"})
	if err != nil {
		t.Fatal(err)
	}
	settled, err := submission.Wait(ctx)
	if err != nil || settled.Submission.Status != "done" || effects.Load() != 1 || requests.Load() != 2 {
		t.Fatal(settled, err, effects.Load(), requests.Load())
	}
}
