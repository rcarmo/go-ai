package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestAssistantProtocolFieldsSignaturesPresenceContextAndReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
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
						assertProtocolMessage(t, actual)
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
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		sub, err := conversation.Submit(bg, Input{Content: "question"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		view, err := h.session.ContextView(bg, conversation.ID(), 0)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, actual := range view.Messages {
			if actual.ResponseID == "response-id" {
				assertProtocolMessage(t, receiptMessage(actual))
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
		assertProtocolMessage(t, receiptMessage(owned))
	})
}

func assertProtocolMessage(t *testing.T, message goai.Message) {
	t.Helper()
	if message.ResponseID != "response-id" || message.ResponseModel != "actual-model" || message.ProviderThinkingLevel != "provider-low" || message.ThinkingLevel != "low" || message.RawStopReason != "raw" || len(message.Content) != 3 {
		t.Fatalf("protocol identity changed %+v", message)
	}
	text, thinking, call := message.Content[0], message.Content[1], message.Content[2]
	if text.TextSignature != "signature" || !text.TextSignaturePresent || !thinking.ThinkingSignaturePresent || thinking.ThinkingSignature != "" || !thinking.RedactedPresent || thinking.Redacted || call.ThoughtSignature != "signed" || !call.ThoughtSignaturePresent || !call.NamespacePresent || call.Namespace != "" || call.Arguments == nil {
		t.Errorf("signature/presence changed text=%q/%v thinking=%q/%v redacted=%v/%v call=%q/%v namespace=%q/%v argsNil=%v", text.TextSignature, text.TextSignaturePresent, thinking.ThinkingSignature, thinking.ThinkingSignaturePresent, thinking.Redacted, thinking.RedactedPresent, call.ThoughtSignature, call.ThoughtSignaturePresent, call.Namespace, call.NamespacePresent, call.Arguments == nil)
	}
}
