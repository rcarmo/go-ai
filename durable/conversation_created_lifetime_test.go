package durable

import (
	"sync/atomic"
	"testing"
)

func TestConversationCreatedNeverReplaysForRetiredAgentOrNilHistoryReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var calls atomic.Int64
		options := Options{ConversationCreated: func(*Tx, Conversation) error { calls.Add(1); return nil }}
		h := openHarness(t, b.store, options)
		root, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}})
		if err != nil {
			t.Fatal(err)
		}
		independent, err := h.CreateConversation(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}})
		if err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 2 {
			t.Fatal(calls.Load())
		}
		for _, conversation := range []*ConversationHandle{independent, root} {
			_, err = h.CommitTasks(bg, conversation.ID(), func(tx *Tx) error {
				doc, ok := agentDocument(tx.state, conversation.ID())
				if !ok {
					return reject("agent missing")
				}
				handle, err := tx.Document(doc.ID)
				if err != nil {
					return err
				}
				return handle.Retire()
			})
			if err != nil {
				t.Fatal(err)
			}
			if conversation.ID() == 1 {
				_, err = h.Root(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}})
			} else {
				err = conversation.Configure(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}})
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if calls.Load() != 2 {
			t.Fatal("metadata retirement replayed creation", calls.Load())
		}
	})
	backends(t, func(t *testing.T, b backend) {
		h := taskTestHarness(t, b.store)
		var raw ID
		_, err := h.CommitTasks(bg, 1, func(tx *Tx) error {
			var err error
			raw, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.CreateConversation(Conversation{ID: raw})
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int64
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), Options{ConversationCreated: func(*Tx, Conversation) error { calls.Add(1); return nil }})
		conversation, err := second.Conversation(bg, raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := conversation.Configure(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}}); err != nil {
			t.Fatal(err)
		}
		if calls.Load() != 0 {
			t.Fatal("historical raw configuration replayed hook", calls.Load())
		}
	})
}
