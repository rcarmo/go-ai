package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func TestCompactionSerializationOmitsSystemImagesAndBoundsToolText(t *testing.T) {
	messages := []MessageReceipt{
		{Role: goai.RoleSystem, Content: []goai.ContentBlock{{Type: "text", Text: "system private"}}},
		{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: "question"}, {Type: "image", Data: "IMAGE_PAYLOAD", MimeType: "image/png"}}},
		{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "reasoning"}, {Type: "text", Text: "answer"}, {Type: "toolCall", ID: "c", Name: "read", Arguments: map[string]any{"path": "/tmp/x", "count": 2}}}},
		{Role: goai.RoleToolResult, Content: []goai.ContentBlock{{Type: "text", Text: strings.Repeat("x", 2500)}}},
	}
	text, err := SerializeConversation(messages, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"[User]: question", "[Assistant thinking]: reasoning", "[Assistant]: answer", "read(count=2, path=\"/tmp/x\")", strings.Repeat("x", 2000), "[... 500 more characters truncated]"} {
		if !strings.Contains(text, want) {
			t.Error("serialization missing", want)
		}
	}
	if strings.Contains(text, "system private") || strings.Contains(text, "IMAGE_PAYLOAD") || strings.Contains(text, strings.Repeat("x", 2001)) {
		t.Fatal("serialization leaked unbounded/control content")
	}
	unicode := truncateCompactionText(strings.Repeat("🙂", 1001), 2000)
	if !strings.HasPrefix(unicode, strings.Repeat("🙂", 1000)) || !strings.Contains(unicode, "2 more characters truncated") {
		t.Fatal("unicode truncation", unicode)
	}
}
func TestCompactionReopenPinsBehaviorButRehydratesTransport(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		entered := make(chan struct{})
		var calls atomic.Int64
		var changed atomic.Bool
		ref, options := setupFakeAPI(t, func(ctx context.Context, model *goai.Model, input *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
			n := calls.Add(1)
			if model.ContextWindow != 128000 || opts.MaxTokens == nil || *opts.MaxTokens != 100 || opts.Temperature == nil || *opts.Temperature != 0.3 {
				t.Error("summary behavior changed", model.ContextWindow, opts)
			}
			if !strings.Contains(input.Messages[0].Content[0].Text, "<conversation>\n[User]: old") || !strings.Contains(input.Messages[0].Content[0].Text, "Additional focus: paths") {
				t.Error("summary transcript", input)
			}
			if n == 2 && (model.BaseURL != "https://new.invalid" || model.APIKey != "runtime-secret") {
				t.Error("transport not rehydrated", model.BaseURL)
			}
			ch := make(chan goai.Event, 1)
			if n == 1 {
				go func() { close(entered); <-ctx.Done(); close(ch) }()
			} else {
				ch <- terminal("pinned summary")
				close(ch)
			}
			return ch
		})
		original := options.Models
		options.Models = func(provider goai.Provider, id string) *goai.Model {
			model := *original(provider, id)
			if changed.Load() {
				model.ContextWindow = 123
				model.BaseURL = "https://new.invalid"
				model.APIKey = "runtime-secret"
			}
			return &model
		}
		first := openHarness(t, b.store, options)
		temperature := 0.3
		conversation, err := first.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{Temperature: &temperature}})
		if err != nil {
			t.Fatal(err)
		}
		_, err = conversation.Commit(bg, func(tx *Tx) error {
			for _, text := range []string{"old", "recent"} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				wire, err := dtoObject(userReceipt(text), tx.limits)
				if err != nil {
					return err
				}
				if err = tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: wire}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		id, err := conversation.Compact(bg, CompactionOptions{KeepRecentTokens: goai.EstimateTextTokens("recent"), MaxTokens: 100, Instructions: "paths"})
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		changed.Store(true)
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		if result := waitPublicTask(t, second, id); result.State.Outcome.Status != "completed" {
			t.Fatal(result)
		}
		state, err := second.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range state.Entries {
			if entry.Kind == "pi.compaction" {
				found = true
				if entry.Value["reason"] != "manual" || entry.Model[0].Timestamp == 0 {
					t.Fatal("compaction metadata missing", entry)
				}
			}
		}
		if !found || calls.Load() != 2 {
			t.Fatal("summary not recovered", found, calls.Load())
		}
	})
}
