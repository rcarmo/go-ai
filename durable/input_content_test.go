package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestRichInputBlocksOwnedQueuedAndPersistedThroughReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			images := 0
			for _, message := range input.Messages {
				if message.Role == goai.RoleUser {
					for _, block := range message.Content {
						if block.Type == "image" && block.MimeType == "image/png" && (block.Data == "AQID" || block.Data == "BAUG") {
							images++
						}
					}
				}
			}
			if images != 2 {
				t.Error("rich context lost queued/idle images", images, input.Messages)
			}
			ch := make(chan goai.Event, 1)
			ch <- terminal("images seen")
			close(ch)
			return ch
		})
		first := openHarness(t, b.store, options)
		conversation := root(t, first, ref)
		passive, err := conversation.Submit(bg, Input{Type: "write", Content: "passive image", Blocks: []goai.ContentBlock{{Type: "image", Data: "AQID", MimeType: "image/png"}}})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, passive)
		if err := first.Close(bg); err != nil {
			t.Fatal(err)
		}
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		conversation, err = second.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		// Queue from the private admission seam without enabling scheduling so the
		// caller's mutation after Submit can be checked before provider dispatch.
		blocks := []goai.ContentBlock{{Type: "image", Data: "BAUG", MimeType: "image/png"}}
		second.mu.Lock()
		handle, err := conversation.submitNewAdmission(bg, Input{Type: "steer", Content: "active image", Blocks: blocks, RequestID: "rich"}, nil)
		second.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		blocks[0].Data = "caller mutation"
		result := waitSubmission(t, handle)
		if result.Submission.Status != "done" {
			t.Fatal(result)
		}
		if err := second.Close(bg); err != nil {
			t.Fatal(err)
		}
		third := openHarness(t, reopenStoreAfterHarnessClose(t, second.session.store), options)
		recovered, err := third.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		view, err := recovered.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		images := 0
		for _, message := range view.Messages {
			for _, block := range message.Content {
				if block.Type == "image" {
					images++
					if block.Data == "caller mutation" {
						t.Fatal("input alias persisted")
					}
				}
			}
		}
		if images != 2 {
			t.Fatal("rich reopen lost images", view)
		}
	})
}
func TestInvalidRichInputRejectsBeforeReservation(t *testing.T) {
	store, _ := NewMemory()
	h := openHarness(t, store, Options{})
	conversation, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: goai.ProviderOpenAI, ID: "unused"}})
	if err != nil {
		t.Fatal(err)
	}
	before := snap(t, h.session.store)
	if _, err := conversation.Submit(bg, Input{Blocks: []goai.ContentBlock{{Type: "toolCall", ID: "call", Name: "forged", Arguments: map[string]any{}}}}); err == nil {
		t.Fatal("user tool authority accepted")
	}
	after := snap(t, h.session.store)
	if after.Seq != before.Seq || after.HighWater != before.HighWater {
		t.Fatal("invalid rich input reserved/wrote")
	}
}
