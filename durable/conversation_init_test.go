package durable

import (
	"errors"
	"strings"
	"testing"
)

func TestConversationInitPinnedCreationHookThenAgentThenInitAtomicRollback(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		order := []string{}
		options := Options{ConversationCreated: func(tx *Tx, conversation Conversation) error {
			state, err := tx.current()
			if err != nil {
				return err
			}
			doc, _ := agentDocument(state, conversation.ID)
			cwd, _ := doc.Value["cwd"].(string)
			order = append(order, "created:"+cwd)
			return nil
		}}
		h := openHarness(t, b.store, options)
		root, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}, Cwd: "/root"})
		if err != nil {
			t.Fatal(err)
		}
		init := func(tx *Tx, id ID) error {
			state, err := tx.current()
			if err != nil {
				return err
			}
			doc, _ := agentDocument(state, id)
			cwd, _ := doc.Value["cwd"].(string)
			order = append(order, "init:"+cwd)
			entry, err := tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: entry, Conversation: id, Kind: "app.init", Value: JSON{"cwd": cwd}})
		}
		child, err := h.CreateConversationWithInit(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}, Cwd: "/child"}, init)
		if err != nil {
			t.Fatal(err)
		}
		if child.ID() == root.ID() {
			t.Fatal("independent id")
		}
		var at ID
		_, err = root.Commit(bg, func(tx *Tx) error {
			var err error
			at, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: at, Conversation: root.ID(), Kind: "app.cut", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		fork, err := root.ForkWithInit(bg, at, &AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}, Cwd: "/fork"}, init)
		if err != nil {
			t.Fatal(err)
		}
		agent, err := fork.Agent(bg)
		if err != nil || agent.Configuration.Cwd != "/fork" || strings.Join(order, "|") != "created:|created:|init:/child|created:/root|init:/fork" {
			t.Fatal(order, agent, err)
		}
		for _, panicCase := range []bool{false, true} {
			before, _ := h.Snapshot(bg)
			_, err = h.CreateConversationWithInit(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}}, func(tx *Tx, id ID) error {
				entry, err := tx.MintID()
				if err != nil {
					return err
				}
				if err := tx.AppendEntry(Entry{ID: entry, Conversation: id, Kind: "app.init-fail", Value: JSON{}}); err != nil {
					return err
				}
				if panicCase {
					panic("private init")
				}
				return errors.New("init failed")
			})
			if err == nil {
				t.Fatal("init failure accepted")
			}
			after, _ := h.Snapshot(bg)
			if after.Seq != before.Seq || len(after.Conversations) != len(before.Conversations) || len(after.Documents) != len(before.Documents) || len(after.Entries) != len(before.Entries) {
				t.Fatal("init not atomic")
			}
			_, err = root.ForkWithInit(bg, at, nil, func(tx *Tx, id ID) error {
				entry, err := tx.MintID()
				if err != nil {
					return err
				}
				if err := tx.AppendEntry(Entry{ID: entry, Conversation: id, Kind: "app.fork-init-fail", Value: JSON{}}); err != nil {
					return err
				}
				if panicCase {
					panic("private fork init")
				}
				return errors.New("fork init failed")
			})
			if err == nil {
				t.Fatal("fork init failure accepted")
			}
			after, _ = h.Snapshot(bg)
			if after.Seq != before.Seq || len(after.Conversations) != len(before.Conversations) || len(after.Documents) != len(before.Documents) || len(after.Entries) != len(before.Entries) {
				t.Fatal("fork init not atomic")
			}
		}
	})
}
