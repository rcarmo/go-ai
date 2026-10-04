// This local deterministic example exercises exported Stream via the registered
// OpenAI provider, a replay-safe host tool and a persistent journal. No live
// credentials or external model server are required.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	_ "github.com/rcarmo/go-ai/inference/provider/openai"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	dir := os.Getenv("DURABLE_EXAMPLE_DIR")
	if dir == "" {
		tmp, err := os.MkdirTemp("", "go-ai-durable-example-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		dir = filepath.Join(tmp, "private")
	}
	var requests atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "text/event-stream")
		if requests.Add(1) == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"type\":\"function\",\"function\":{\"name\":\"sum\",\"arguments\":\"{\\\"a\\\":2,\\\"b\\\":3}\"}}]},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
		} else {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"The sum is 5.\"},\"finish_reason\":null}]}\n\ndata: {\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
		}
	}))
	defer server.Close()
	registry := durable.NewRegistry()
	if err := registry.Register(durable.ToolRegistration{Definition: goai.Tool{Name: "sum", Description: "Add two bounded integers", Parameters: json.RawMessage(`{"type":"object","properties":{"a":{"type":"integer","minimum":0,"maximum":100},"b":{"type":"integer","minimum":0,"maximum":100}},"required":["a","b"],"additionalProperties":false}`)}, Implementation: "example.sum", Version: 1, ReplaySafe: true, Execute: func(ctx context.Context, args durable.JSON, api *durable.ToolAPI) (durable.ToolResult, error) {
		a, err := args["a"].(json.Number).Int64()
		if err != nil {
			return durable.ToolResult{}, err
		}
		b, err := args["b"].(json.Number).Int64()
		return durable.ToolResult{Content: fmt.Sprint(a + b)}, err
	}}); err != nil {
		return err
	}
	model := &goai.Model{ID: "local-model", Provider: goai.ProviderOpenAI, Api: goai.ApiOpenAICompletions, BaseURL: server.URL, ContextWindow: 8192, MaxTokens: 256}
	store, err := durable.OpenJournal(dir, durable.JournalOptions{})
	if err != nil {
		return err
	}
	ctx := context.Background()
	h, err := durable.Open(ctx, store, durable.Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model { return model }, RequestOptions: func(context.Context, durable.ModelRef) (*goai.StreamOptions, error) {
		return &goai.StreamOptions{APIKey: "fake-local-only"}, nil
	}})
	if err != nil {
		return err
	}
	defer h.Close(ctx)
	root, err := h.Root(ctx, durable.AgentChange{Model: durable.ModelRef{Provider: model.Provider, ID: model.ID}})
	if err != nil {
		return err
	}
	sub, err := root.Submit(ctx, durable.Input{Content: "Add 2 and 3", RequestID: "example-sum"})
	if err != nil {
		return err
	}
	settled, err := sub.Wait(ctx)
	if err != nil {
		return err
	}
	if settled.Submission.Status != "done" || settled.Message == nil {
		return fmt.Errorf("generation failed")
	}
	for _, block := range settled.Message.Content {
		if block.Type == "text" {
			fmt.Println(block.Text)
		}
	}
	return nil
}
