package durable

import (
	"context"
	"encoding/base64"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func TestTool120ErrorResultRetainsLargeImageTranscript(t *testing.T) {
	checkTool120ErrorImage(t, false)
}

func TestTool120LargeImagePublicReconnectWithinQuota(t *testing.T) {
	checkTool120ErrorImage(t, true)
}

func checkTool120ErrorImage(t *testing.T, reconnect bool) {
	backends(t, func(t *testing.T, b backend) {
		if reconnect {
			// More bytes per observer without changing total retained memory:
			// reduce observer/task/page capacity using a persisted supported limit.
			limits := DefaultLimits()
			limits.MaxPage = 8
			if err := b.store.Close(bg); err != nil {
				t.Fatal(err)
			}
			var err error
			if _, ok := b.store.(*MemoryStorage); ok {
				b.store, err = OpenMemory(MemoryOptions{Limits: &limits})
			} else {
				b.store, err = OpenJournal(t.TempDir()+"/reconnect", JournalOptions{Limits: &limits})
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		registry := NewRegistry()
		data := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("hello", 40000)))
		var effects, seen, requests atomic.Int64
		tool := wrapRegistration("failed_image")
		tool.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{IsError: true, Blocks: []goai.ContentBlock{{Type: "text", Text: "partial"}, {Type: "image", MimeType: "image/png", Data: data}}}, nil
		}
		if err := registry.Register(tool); err != nil {
			t.Fatal(err)
		}
		api := goai.Api("failed-large-image-120")
		goai.RegisterApi(&goai.ApiProvider{Api: api, Stream: func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonStop, Content: []goai.ContentBlock{{Type: "text", Text: "done"}}}
			if requests.Add(1) == 1 {
				message.StopReason = goai.StopReasonToolUse
				message.Content = []goai.ContentBlock{{Type: "toolCall", ID: "image", Name: "failed_image", Arguments: map[string]any{}}}
			} else {
				found := false
				for _, m := range input.Messages {
					if m.Role != goai.RoleToolResult {
						continue
					}
					for _, block := range m.Content {
						if block.Type == "image" && block.Data == data {
							found = true
							if !m.IsError {
								t.Error("executor error lost")
							}
						}
					}
				}
				if !found {
					t.Error("large error image missing")
				}
				seen.Add(1)
			}
			ch := make(chan goai.Event, 1)
			ch <- &goai.DoneEvent{Reason: message.StopReason, Message: message}
			close(ch)
			return ch
		}})
		defer goai.UnregisterApi(api)
		options := Options{Registry: registry, Models: func(goai.Provider, string) *goai.Model {
			return &goai.Model{Provider: "test", ID: "model", Api: api, Input: []string{"text", "image"}, ContextWindow: 10000, MaxTokens: 100}
		}}
		h := openHarness(t, b.store, options)
		conv, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: "test", ID: "model"}})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conv.Submit(bg, Input{Content: "read image"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 10*time.Second)
		defer cancel()
		result, err := sub.Wait(ctx)
		if err != nil || result.Submission.Status != "done" || effects.Load() != 1 || seen.Load() != 1 {
			t.Fatal(result, err, effects.Load(), seen.Load())
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		store := reopenStoreAfterHarnessClose(t, b.store)
		h2 := openHarness(t, store, options)
		reopened, err := h2.Conversation(bg, conv.ID())
		if err != nil {
			t.Fatal(err)
		}
		if reconnect {
			stream, err := h2.WatchEvents(bg, conv.ID())
			if err != nil {
				t.Fatal("public reconnect admission", err)
			}
			defer stream.Stop()
			snapshot, err := stream.Snapshot()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, m := range snapshot.Messages {
				for _, block := range m.Content {
					if block.Type == "image" && block.Data == data {
						found = true
					}
				}
			}
			if !found {
				t.Fatal("public reconnect snapshot lost image")
			}
		}
		sub, err = reopened.Submit(bg, Input{Content: "inspect failed image"})
		if err != nil {
			t.Fatal(err)
		}
		result, err = sub.Wait(ctx)
		if err != nil || result.Submission.Status != "done" || effects.Load() != 1 || seen.Load() != 2 {
			t.Fatal(result, err, effects.Load(), seen.Load())
		}
	})
}
