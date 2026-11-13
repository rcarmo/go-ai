package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"os"
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
	for _, want := range []string{"[User]: question", "[Assistant]: answer", "read(count=2, path=\"/tmp/x\")", strings.Repeat("x", 2000), "[... 500 more characters truncated]"} {
		if !strings.Contains(text, want) {
			t.Error("serialization missing", want)
		}
	}
	if strings.Contains(text, "reasoning") || strings.Contains(text, "[Assistant thinking]") || strings.Contains(text, "system private") || strings.Contains(text, "IMAGE_PAYLOAD") || strings.Contains(text, strings.Repeat("x", 2001)) {
		t.Fatal("serialization leaked unbounded/control content")
	}
	unicode := truncateCompactionText(strings.Repeat("🙂", 1001), 2000)
	if !strings.HasPrefix(unicode, strings.Repeat("🙂", 1000)) || !strings.Contains(unicode, "2 more characters truncated") {
		t.Fatal("unicode truncation", unicode)
	}
}

func TestSourceCompactionSerializationOmitsThinking(t *testing.T) {
	// Literal harness-compaction.test.ts:351-370 at f9a2636; every original
	// positive and negative assertion is retained.
	empty, hidden, hello := "", "hidden", "hello"
	messages := []MessageReceipt{
		{Role: goai.RoleSystem, ContentText: &empty, Sections: map[string]*string{"s": &hidden}},
		{Role: goai.RoleUser, ContentText: &hello},
		{Role: goai.RoleAssistant, Content: []goai.ContentBlock{{Type: "thinking", Thinking: "hmm"}, {Type: "text", Text: "sure"}, {Type: "toolCall", ID: "c", Name: "read", Arguments: JSON{"path": "a.ts"}}}},
		{Role: goai.RoleToolResult, ToolCallID: "c", Content: []goai.ContentBlock{{Type: "text", Text: strings.Repeat("y", 2500)}}},
	}
	text, err := SerializeConversation(messages, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, absent := range []string{"hidden", "hmm", "[Assistant thinking]"} {
		if strings.Contains(text, absent) {
			t.Errorf("source transcript retained %q", absent)
		}
	}
	for _, present := range []string{"[User]: hello", "[Assistant]: sure", `[Assistant tool calls]: read(path="a.ts")`, "[Tool result]: " + strings.Repeat("y", 2000) + "\n\n[... 500 more characters truncated]"} {
		if !strings.Contains(text, present) {
			t.Errorf("source transcript omitted %q", present)
		}
	}
}

// Literal source string arm from f9a2636 src/harness/compaction.ts:385-417.
func TestCompactionSerializationPreservesSourceStringContent(t *testing.T) {
	question, empty, system := "question", "", "private"
	text, err := SerializeConversation([]MessageReceipt{
		{Role: goai.RoleSystem, ContentText: &system},
		{Role: goai.RoleUser, ContentText: &question},
		{Role: goai.RoleUser, ContentText: &empty},
	}, DefaultLimits())
	if err != nil || text != "[User]: question" {
		t.Fatalf("source string transcript = %q, err=%v", text, err)
	}
}

func TestCompactionPromptsMatchPinnedSourceLiterals(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-durable-v150/compaction-prompts.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct{ Target, System, User string }
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Target != "f9a26365568127461c47a07a37dcb42c140a3ac5" {
		t.Fatal("wrong source target")
	}
	if compactionSystemPrompt != fixture.System {
		t.Error("summarization system prompt differs from source")
	}
	for _, instructions := range []string{"", "preserve paths"} {
		want := "<conversation>\n[User]: hello\n</conversation>\n\n" + fixture.User
		if instructions != "" {
			want += "\n\nAdditional focus: " + instructions
		}
		if got := compactionRequestText("[User]: hello", instructions); got != want {
			t.Errorf("source summary request differs: %q", got)
		}
	}
}

func TestCompactionReopenPinsBehaviorButRehydratesTransport(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
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
				if legacyEntryValue(entry)["reason"] != "manual" || entry.Model[0].Timestamp == 0 {
					t.Fatal("compaction metadata missing", entry)
				}
			}
		}
		if !found || calls.Load() != 2 {
			t.Fatal("summary not recovered", found, calls.Load())
		}
	})
}
