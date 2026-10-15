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
func TestDurable120PublicCodemodeConsumer(t *testing.T)      { testPublicCodemodeConsumer120(t, false) }
func TestDurable120PublicLargeCodemodeConsumer(t *testing.T) { testPublicCodemodeConsumer120(t, true) }
func testPublicCodemodeConsumer120(t *testing.T, large bool) {
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
	writer := &largeConsumerWriter120{}
	api := goai.Api("public-codemode-consumer-120")
	code := `await Promise.all([tools.echo({}),tools.echo({})]).then(v=>text(v.join(" + ")));`
	if large {
		code = `// @options: {"max_output_tokens":1000000}
for(let i=0;i<4;i++)text("x".repeat(700000));`
	}
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "public-call", Name: "codemode", Arguments: map[string]any{"code": code}}}}
			ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
		} else {
			found := false
			for _, message := range input.Messages {
				if message.Role == goai.RoleToolResult && message.ToolName == "codemode" {
					found = true
					text := goai.ContentText(message.Content)
					valid := strings.Contains(text, "Output:\npublic API + public API")
					if large {
						valid = len(text) < 65536 && strings.Contains(text, "Full output: /owned/output.txt")
					}
					if message.IsError || !valid {
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
	harness, err := durable.Open(ctx, storage, durable.Options{Registry: registry, Env: func(context.Context, durable.EnvTarget) (durable.ExecutionEnvironment, error) { return writer, nil }, Models: func(goai.Provider, string) *goai.Model { return model }})
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
	if err != nil || settled.Submission.Status != "done" || effects.Load() != map[bool]int64{false: 2, true: 0}[large] || requests.Load() != 2 {
		t.Fatal(settled, err, effects.Load(), requests.Load())
	}
	if large && (writer.writes.Load() != 1 || len(writer.full) < 2800000 || strings.Count(writer.full, strings.Repeat("x", 700000)) != 4) {
		t.Error("full spill lost content", writer.writes.Load(), len(writer.full))
	}
}

type largeConsumerWriter120 struct {
	writes atomic.Int64
	full   string
}

func (*largeConsumerWriter120) Cwd() string { return "/owned" }
func (w *largeConsumerWriter120) WriteCodemodeOutput(_ context.Context, _ durable.ID, _ string, name string, data []byte) (string, error) {
	w.writes.Add(1)
	w.full = string(data)
	return "/owned/" + name, nil
}
