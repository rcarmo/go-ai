package tools

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestPerConversationEnvironmentFactoryCwdReadsAndReopen(t *testing.T) {
	base := t.TempDir()
	firstDir, secondDir := filepath.Join(base, "first"), filepath.Join(base, "second")
	fallback := LocalEnv(filepath.Join(base, "fallback"))
	registry := durable.NewRegistry()
	if err := registry.Register(Write(fallback)); err != nil {
		t.Fatal(err)
	}
	api := goai.Api("env-" + strings.ReplaceAll(t.Name(), "/", "-"))
	var requests, factories atomic.Int64
	var reader *durable.InvocationReader
	goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, model *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		requests.Add(1)
		result := &goai.Message{Role: goai.RoleAssistant, Api: model.Api, Provider: model.Provider, Model: model.ID, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "written"}}}
		if input.Messages[len(input.Messages)-1].Role != goai.RoleToolResult {
			result.StopReason = goai.StopReasonToolUse
			result.Content = []goai.ContentBlock{{Type: "toolCall", ID: "write-call", Name: "write", Arguments: map[string]any{"path": "result.txt", "content": "owned environment"}}}
		}
		ch := make(chan goai.Event, 1)
		ch <- &goai.DoneEvent{Reason: result.StopReason, Message: result}
		close(ch)
		return ch
	}})
	t.Cleanup(func() { goai.UnregisterApi(api) })
	options := durable.Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model {
		return &goai.Model{ID: "test", Provider: goai.ProviderOpenAI, Api: api, ContextWindow: 128000, MaxTokens: 100}
	}, Env: func(ctx context.Context, target durable.EnvTarget) (durable.ExecutionEnvironment, error) {
		factories.Add(1)
		reader = target.Read
		if _, err := target.Read.ContextView(ctx, target.Conversation, 0); err != nil {
			return nil, err
		} // factory is off-line and reentrant.
		return LocalEnv(target.Cwd), nil
	}}
	path := filepath.Join(base, "journal")
	store, err := durable.OpenJournal(path, durable.JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := durable.Open(context.Background(), store, options)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(context.Background())
	ref := durable.ModelRef{Provider: goai.ProviderOpenAI, ID: "test"}
	conversation, err := h.Root(context.Background(), durable.AgentChange{Model: ref, Cwd: firstDir})
	if err != nil {
		t.Fatal(err)
	}
	wait := func(h *durable.Harness, c *durable.ConversationHandle) {
		t.Helper()
		sub, err := c.Submit(context.Background(), durable.Input{Content: "write"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		settled, err := sub.Wait(ctx)
		if err != nil || settled.Submission.Status != "done" {
			t.Fatal(settled, err)
		}
	}
	wait(h, conversation)
	if data, err := os.ReadFile(filepath.Join(firstDir, "result.txt")); err != nil || string(data) != "owned environment" {
		t.Fatal(string(data), err)
	}
	if _, err := reader.ContextView(context.Background(), reader.ConversationID(), 0); !errors.Is(err, durable.ErrSealed) {
		t.Fatal("escaped factory reader live", err)
	}
	if err := conversation.Configure(context.Background(), durable.AgentChange{Model: ref, Cwd: secondDir}); err != nil {
		t.Fatal(err)
	}
	id := conversation.ID()
	if err := h.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	store, err = durable.OpenJournal(path, durable.JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := durable.Open(context.Background(), store, options)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
	recovered, err := second.Conversation(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	wait(second, recovered)
	if data, err := os.ReadFile(filepath.Join(secondDir, "result.txt")); err != nil || string(data) != "owned environment" {
		t.Fatal(string(data), err)
	}
	if factories.Load() != 6 || requests.Load() != 4 {
		t.Fatal("factory uses", factories.Load(), requests.Load())
	}
	if _, err := os.Stat(filepath.Join(fallback.Cwd(), "result.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("factory bypassed", err)
	}
	bytes, err := os.ReadFile(filepath.Join(path, "journal.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bytes), "fallback") {
		t.Fatal("process-local environment persisted")
	}
}
