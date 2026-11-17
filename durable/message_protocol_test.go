package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestAssistantProtocolFieldsSignaturesPresenceContextAndReopen(t *testing.T) {
	for _, explicitRegistry := range []bool{false, true} {
		name := "default-empty-registry"
		if explicitRegistry {
			name = "explicit-empty-registry"
		}
		t.Run(name, func(t *testing.T) { testAssistantProtocolFields(t, explicitRegistry) })
	}
}

func testAssistantProtocolFields(t *testing.T, explicitRegistry bool) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		message := goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, ResponseID: "response-id", ResponseModel: "actual-model", ProviderThinkingLevel: "provider-low", ThinkingLevel: "low", RawStopReason: "raw", Content: []goai.ContentBlock{{Type: "text", Text: "text", TextSignature: "signature", TextSignaturePresent: true}, {Type: "thinking", Thinking: "thought", ThinkingSignaturePresent: true, RedactedPresent: true}, {Type: "toolCall", ID: "call", Name: "missing", Arguments: map[string]any{}, ThoughtSignature: "signed", ThoughtSignaturePresent: true, NamespacePresent: true}}}
		calls := 0
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			if calls == 1 {
				copy := message
				ch <- &goai.DoneEvent{Reason: message.StopReason, Message: &copy}
			} else {
				found := false
				for _, actual := range input.Messages {
					if actual.Role == goai.RoleAssistant {
						assertProtocolMessage(t, actual, goai.ThinkingOff)
						found = true
					}
				}
				if !found {
					t.Error("assistant missing in successor context")
				}
				ch <- terminal("answer")
			}
			close(ch)
			return ch
		})
		if explicitRegistry {
			options.Registry = NewRegistry()
		}
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		sub, err := conversation.Submit(bg, Input{Content: "question"})
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, sub)
		if result.Message == nil || goai.ContentText(result.Message.Content) != "answer" || calls != 2 {
			t.Fatal("unavailable tool round did not reach the answer", result, calls)
		}
		_, err = h.Commit(bg, func(tx *Tx) (any, error) {
			page, err := tx.ScanTasks(TaskQuery{Conversation: conversation.ID(), Kind: "pi.tool"}, 20, nil)
			if err != nil {
				return nil, err
			}
			var input JSON
			if len(page.Items) == 1 && page.Items[0].Input != nil {
				input = generationObject(page.Items[0].Input.Value)
			}
			if len(page.Items) != 1 || input["callId"] != "call" || input["unoffered"] != true {
				t.Error("unavailable call must retain its own flagged task", page.Items)
			}
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		reopened, err := h.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		view, err := reopened.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, actual := range view.Messages {
			if actual.ResponseID == "response-id" {
				assertProtocolMessage(t, receiptMessage(actual), goai.ThinkingOff)
				found = true
			}
		}
		if !found {
			t.Fatal("protocol receipt lost on reopen")
		}
		receipt, err := contributionReceipt(message, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		value, err := dtoObject(receipt, DefaultLimits())
		if err != nil {
			t.Fatal(err)
		}
		var owned MessageReceipt
		if err := fromObject(value, &owned, DefaultLimits()); err != nil {
			t.Fatal(err)
		}
		if err := restoreReceiptArguments(&owned, DefaultLimits()); err != nil {
			t.Fatal(err)
		}
		assertProtocolMessage(t, receiptMessage(owned), "low")
	})
}

func assertProtocolMessage(t *testing.T, message goai.Message, wantThinking goai.ModelThinkingLevel) {
	t.Helper()
	if message.ResponseID != "response-id" || message.ResponseModel != "actual-model" || message.ProviderThinkingLevel != "provider-low" || message.ThinkingLevel != wantThinking || message.RawStopReason != "raw" || len(message.Content) != 3 {
		t.Errorf("protocol identity changed: response=%q model=%q providerThinking=%q thinking=%q stop=%q content=%d", message.ResponseID, message.ResponseModel, message.ProviderThinkingLevel, message.ThinkingLevel, message.RawStopReason, len(message.Content))
		if len(message.Content) != 3 {
			return
		}
	}
	text, thinking, call := message.Content[0], message.Content[1], message.Content[2]
	if text.TextSignature != "signature" || !text.TextSignaturePresent || !thinking.ThinkingSignaturePresent || thinking.ThinkingSignature != "" || !thinking.RedactedPresent || thinking.Redacted || call.ThoughtSignature != "signed" || !call.ThoughtSignaturePresent || !call.NamespacePresent || call.Namespace != "" || call.Arguments == nil {
		t.Errorf("signature/presence changed text=%q/%v thinking=%q/%v redacted=%v/%v call=%q/%v namespace=%q/%v argsNil=%v", text.TextSignature, text.TextSignaturePresent, thinking.ThinkingSignature, thinking.ThinkingSignaturePresent, thinking.Redacted, thinking.RedactedPresent, call.ThoughtSignature, call.ThoughtSignaturePresent, call.Namespace, call.NamespacePresent, call.Arguments == nil)
	}
}
