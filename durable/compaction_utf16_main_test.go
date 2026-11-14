package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	goai "github.com/rcarmo/go-ai"
)

func TestCompactionUTF16FormattingAndRequestOwnership(t *testing.T) {
	high := toolUTF16Scalar(t, `"\ud800"`)
	low := toolUTF16Scalar(t, `"\udfff"`)
	call := goai.ContentBlock{Type: "toolCall", ID: "c", Name: "leaf"}
	call.SetStreamingArguments(map[string]any{high.WTF8(): low}, "")
	diagnostic := goai.ContentBlock{}
	diagnostic.SetToolArgumentDiagnostic("error " + high.WTF8())
	messages := []MessageReceipt{{Role: goai.RoleAssistant, Content: []goai.ContentBlock{call}}, {Role: goai.RoleToolResult, Content: []goai.ContentBlock{diagnostic}}}
	source, err := SerializeConversation(messages, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source, high.WTF8()+`="\udfff"`) || !strings.Contains(source, "error "+high.WTF8()) {
		t.Fatal("formatted units lost")
	}
	focus := "focus"
	block := compactionRequestBlock(source, &focus, true)
	if !block.HasToolArgumentTranscript() || !strings.Contains(block.Text, source) {
		t.Fatal("request lost source ownership")
	}
	bad := append([]MessageReceipt(nil), messages...)
	bad = append(bad, MessageReceipt{Role: goai.RoleUser, Content: []goai.ContentBlock{{Type: "text", Text: high.WTF8()}}})
	if _, err = SerializeConversation(bad, DefaultLimits()); err == nil {
		t.Fatal("native arguments widened unrelated text")
	}
	l := DefaultLimits()
	l.MaxStringBytes = 8
	if _, err = SerializeConversation(messages, l); err == nil {
		t.Fatal("string cap bypassed")
	}
	prefix := strings.Repeat("x", 1999)
	cut := truncateCompactionText(prefix+high.WTF8()+"end", 2000)
	if !strings.HasPrefix(cut, prefix+high.WTF8()) || !strings.Contains(cut, "3 more characters") {
		t.Fatal("isolated unit truncation count")
	}
	cut = truncateCompactionText(prefix+"😀"+low.WTF8()+"end", 2000)
	want := prefix + toolUTF16Scalar(t, `"\ud83d"`).WTF8() + "\n\n[... 5 more characters truncated]"
	if cut != want || utf8.ValidString(cut) {
		t.Fatal("source UTF-16 slice boundary changed")
	}
}

func TestCompactionUTF16MainExecutionReopen(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		high := toolUTF16Scalar(t, `"\ud800"`)
		low := toolUTF16Scalar(t, `"\udfff"`)
		call := goai.ContentBlock{Type: "toolCall", ID: "c", Name: "leaf"}
		call.SetStreamingArguments(map[string]any{high.WTF8(): low}, "")
		diagnostic := goai.ContentBlock{}
		diagnostic.SetToolArgumentDiagnostic("error " + high.WTF8())
		entered := make(chan struct{})
		var requests atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			block := input.Messages[0].Content[0]
			if !block.HasToolArgumentTranscript() || !strings.Contains(block.Text, high.WTF8()+`="\udfff"`) || !strings.Contains(block.Text, "error "+high.WTF8()) {
				t.Error("summary prompt lost exact units/owner")
			}
			events := make(chan goai.Event, 1)
			if requests.Add(1) == 1 {
				go func() { close(entered); <-ctx.Done(); close(events) }()
			} else {
				events <- terminal("Remember earlier work.")
				close(events)
			}
			return events
		})
		first := openHarness(t, b.store, options)
		conversation := root(t, first, ref)
		_, err := conversation.Commit(bg, func(tx *Tx) error {
			for _, message := range []MessageReceipt{userReceipt("old question"), {Role: goai.RoleAssistant, Content: []goai.ContentBlock{call}, StopReason: goai.StopReasonToolUse}, {Role: goai.RoleToolResult, ToolCallID: "c", ToolName: "leaf", IsError: true, ErrorCode: "invalid_arguments", Content: []goai.ContentBlock{diagnostic}}, userReceipt("recent question")} {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				value, err := dtoObject(message, tx.limits)
				if err != nil {
					return err
				}
				if err = tx.AppendEntry(Entry{ID: id, Conversation: conversation.ID(), Kind: "message", Value: value}); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		configureCompactionTestPolicy(t, conversation, compactionTestInt(goai.EstimateMessageTokens(receiptMessage(userReceipt("recent question")))), compactionTestInt((100*5+3)/4))
		id, err := conversation.Compact(bg, nil)
		if err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, entered)
		if err = first.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, utf16ReopenStore(t, b.store), options)
		record := waitPublicTask(t, second, id)
		if record.State.Outcome.Status != "completed" || requests.Load() != 2 {
			t.Fatal("summary recovery failed", record.State.Outcome, requests.Load())
		}
		reopened, err := second.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		view, err := reopened.ContextView(bg, 0)
		if err != nil || len(view.Messages) != 2 || !strings.Contains(view.Messages[0].Content[0].Text, "Remember") {
			t.Fatal("compacted context changed", view, err)
		}
	})
}
