//go:build unix

package tools

import (
	"context"
	"encoding/json"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodingToolsProductionHTTPJournalAndReopen(t *testing.T) {
	directory := t.TempDir()
	env := LocalEnv(directory)
	registry := durable.NewRegistry()
	for _, registration := range []durable.ToolRegistration{Read(env), Write(env), Edit(env), Bash(env)} {
		if err := registry.Register(registration); err != nil {
			t.Fatal(err)
		}
	}
	type call struct {
		name string
		args any
	}
	calls := []call{{"write", durable.JSON{"path": "answer.txt", "content": "hello\n"}}, {"edit", durable.JSON{"path": "answer.txt", "edits": []any{durable.JSON{"oldText": "hello", "newText": "world"}}}}, {"read", durable.JSON{"path": "answer.txt"}}, {"bash", durable.JSON{"command": "cat answer.txt; printf stderr >&2"}}}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content any    `json:"content"`
			}
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		index := int(requests.Add(1)) - 1
		if index > 0 {
			found := false
			for _, message := range payload.Messages {
				if message.Role == "tool" {
					found = true
				}
			}
			if !found {
				t.Error("successor lost tool result")
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		if index < len(calls) {
			args, _ := json.Marshal(calls[index].args)
			chunk, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": fmt.Sprintf("call-%d", index), "type": "function", "function": map[string]any{"name": calls[index].name, "arguments": string(args)}}}}}}})
			fmt.Fprintf(w, "data: %s\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", chunk)
		} else {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"done\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}
	}))
	defer server.Close()
	model := &goai.Model{ID: "coding-test", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: server.URL, ContextWindow: 128000, MaxTokens: 256}
	options := durable.Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, durable.ModelRef) (*goai.StreamOptions, error) {
		return &goai.StreamOptions{APIKey: "local-test"}, nil
	}}
	path := filepath.Join(directory, "journal")
	store, err := durable.OpenJournal(path, durable.JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := durable.Open(context.Background(), store, options)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close(context.Background())
	root, err := h.Root(context.Background(), durable.AgentChange{Model: durable.ModelRef{Provider: model.Provider, ID: model.ID}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := root.Submit(context.Background(), durable.Input{Content: "edit and read", RequestID: "coding-tools"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	settled, err := sub.Wait(ctx)
	if err != nil || settled.Submission.Status != "done" {
		t.Fatal(settled, err)
	}
	content, err := os.ReadFile(filepath.Join(directory, "answer.txt"))
	if err != nil || string(content) != "world\n" {
		t.Fatal(string(content), err)
	}
	snapshot, err := h.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	results := map[string]string{}
	for _, entry := range snapshot.Entries {
		if entry.Value["role"] == string(goai.RoleToolResult) {
			encoded, _ := json.Marshal(entry.Value)
			var message durable.MessageReceipt
			if err := json.Unmarshal(encoded, &message); err != nil {
				t.Fatal(err)
			}
			if message.IsError {
				t.Fatal("tool failed", message)
			}
			for _, block := range message.Content {
				results[message.ToolName] += block.Text
			}
		}
	}
	if len(results) != 4 || !strings.Contains(results["read"], "world") || !strings.Contains(results["bash"], "world") || !strings.Contains(results["bash"], "stderr") {
		t.Fatal("tool receipts", results)
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := durable.OpenJournal(path, durable.JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := durable.Open(ctx, reopened, options)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close(context.Background())
	recovered, err := second.Submission(ctx, sub.ID())
	if err != nil {
		t.Fatal(err)
	}
	again, err := recovered.Wait(ctx)
	if err != nil || again.Submission.Status != "done" || requests.Load() != 5 {
		t.Fatal("reopen replayed tools/model", again, err, requests.Load())
	}
}
