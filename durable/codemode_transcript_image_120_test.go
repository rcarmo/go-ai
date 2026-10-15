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

func TestCodemode120LargeImageTranscriptProviderAndReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		data := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("large-image", 40000)))
		var starts, requests, seen, hookSeen atomic.Int64
		registry := NewRegistry()
		extension, err := NewCodemodeExtension(imageChunkSandbox120{data, &starts})
		if err != nil {
			t.Fatal(err)
		}
		extension.Hooks.BeforeRequest = func(_ context.Context, messages []MessageReceipt, _ *HookAPI) ([]MessageReceipt, error) {
			for i, message := range messages {
				for j, block := range message.Content {
					if block.Type == "image" {
						if block.Data != data {
							t.Error("hook image not hydrated")
						}
						hookSeen.Add(1)
						messages[i].Content[j].Data = data
					}
				}
			}
			return messages, nil
		}
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "script-image", Name: "codemode", Arguments: map[string]any{"code": "image"}}}}}
			} else {
				for _, message := range input.Messages {
					if message.Role != goai.RoleToolResult {
						continue
					}
					for _, block := range message.Content {
						if block.Type == "image" {
							if block.Data != data || block.MimeType != "image/png" {
								t.Error("provider image not rehydrated")
							}
							seen.Add(1)
						}
					}
				}
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		events, err := h.WatchEvents(bg, conv.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer events.Stop()
		eventImage := make(chan string, 16)
		if err = events.Start(func(_ context.Context, batch []AgentEvent) error {
			for _, event := range batch {
				if event.Message != nil {
					for _, block := range event.Message.Content {
						if block.Type == "image" {
							eventImage <- block.Data
						}
					}
				}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		sub, err := conv.Submit(bg, Input{Content: "image"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 10*time.Second)
		defer cancel()
		result, err := sub.Wait(ctx)
		if err != nil || result.Submission.Status != "done" || seen.Load() != 1 || starts.Load() != 1 {
			t.Fatal(result, err, seen.Load(), starts.Load())
		}
		select {
		case actual := <-eventImage:
			if actual != data {
				t.Fatal("event image not materialised")
			}
		case <-ctx.Done():
			t.Fatal("image event missing")
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		chunks := 0
		for _, doc := range state.Documents {
			if doc.Kind == codemodeImageChunkKind && doc.Scope == "conversation" {
				chunks++
				if doc.Retired {
					t.Fatal("transcript chunk retired with tool")
				}
			}
		}
		if chunks < 2 {
			t.Fatal("transcript payload not chunked", chunks)
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
		reopenedState, err := h2.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := conversationSnapshot(reopenedState, conv.ID(), h2.session.limits); err != nil {
			t.Fatal(err)
		}
		// The public reconnect reserves baseline demand from the shared budget.
		reconnect, err := h2.WatchEvents(bg, conv.ID())
		if err != nil {
			t.Fatal("default-limit public reconnect", err)
		}
		defer reconnect.Stop()
		snapshot, err := reconnect.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, message := range snapshot.Messages {
			for _, block := range message.Content {
				if block.Type == "image" {
					found = true
					if block.Data != data {
						t.Fatal("snapshot image missing")
					}
				}
			}
		}
		if !found {
			t.Fatal("snapshot transcript lacks image")
		}
		for i := range snapshot.Messages {
			snapshot.Messages[i].Content = nil
		}
		again, err := reconnect.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		found = false
		for _, message := range again.Messages {
			for _, block := range message.Content {
				if block.Type == "image" && block.Data == data {
					found = true
				}
			}
		}
		if !found {
			t.Fatal("snapshot aliases registration")
		}
		sub, err = reopened.Submit(bg, Input{Content: "inspect retained image"})
		if err != nil {
			t.Fatal(err)
		}
		result, err = sub.Wait(ctx)
		if err != nil || result.Submission.Status != "done" || seen.Load() != 2 || starts.Load() != 1 || hookSeen.Load() != 2 {
			t.Fatal(result, err, seen.Load(), starts.Load())
		}
	})
}
