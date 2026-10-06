//go:build unix

package tools

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBashFailedTruncatedOutputPublishesSpillTailAndOriginalError(t *testing.T) {
	registry := durable.NewRegistry()
	if err := registry.Register(Bash(LocalEnv(t.TempDir()))); err != nil {
		t.Fatal(err)
	}
	api := goai.Api("bash-failure-receipt")
	calls := 0
	spill := ""
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls++
		ch := make(chan goai.Event, 1)
		message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}
		if calls == 1 {
			message.StopReason = goai.StopReasonToolUse
			message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "bash-call", Name: "bash", Arguments: map[string]any{"command": "for i in $(seq 1 3000); do echo line-$i; done; exit 7"}}}
		}
		ch <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
		close(ch)
		return ch
	}})
	defer goai.UnregisterApi(api)
	store, err := durable.NewMemory()
	if err != nil {
		t.Fatal(err)
	}
	h, err := durable.Open(context.Background(), store, durable.Options{Registry: registry, Models: func(p goai.Provider, id string) *goai.Model {
		return &goai.Model{Api: api, Provider: p, ID: id, ContextWindow: 128000, MaxTokens: 100}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(context.Background())
	conversation, err := h.CreateConversation(context.Background(), durable.AgentChange{Model: durable.ModelRef{Provider: goai.ProviderOpenAI, ID: "bash-test"}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := conversation.Submit(context.Background(), durable.Input{Content: "run"})
	if err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := sub.Wait(deadline)
	if err != nil || result.Submission.Status != "done" {
		t.Fatal(result, err)
	}
	state, err := h.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range state.Entries {
		if entry.Value["role"] != string(goai.RoleToolResult) {
			continue
		}
		var receipt durable.MessageReceipt
		data, err := json.Marshal(entry.Value)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &receipt); err != nil {
			t.Fatal(err)
		}
		text := ""
		for _, block := range receipt.Content {
			text += block.Text
		}
		if !receipt.IsError || !strings.HasPrefix(text, "line-1001\n") || !strings.Contains(text, "[error] Command exited with code 7") {
			t.Fatal("bash failed output changed", text[:min(len(text), 100)])
		}
		for _, diagnostic := range receipt.Diagnostics {
			if diagnostic.Code == "full_output" {
				spill = strings.TrimPrefix(diagnostic.Message, "Full output: ")
			}
		}
	}
	if spill == "" {
		t.Fatal("spill diagnostic lost")
	}
	defer os.Remove(spill)
	data, err := os.ReadFile(spill)
	if err != nil || !strings.HasPrefix(string(data), "line-1\n") || !strings.Contains(string(data), "line-3000\n") {
		t.Fatal("spill incomplete", err)
	}
}
