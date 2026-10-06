package durable

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestConversationCreatedPinnedRawOwnedForkOrderAndRollback(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		seen := []string{}
		failure := false
		panicNext := false
		options := Options{ConversationCreated: func(tx *Tx, conversation Conversation) error {
			candidate, err := tx.current()
			if err != nil {
				return err
			}
			kinds := map[string]bool{}
			for _, doc := range candidate.Documents {
				if doc.Scope == "conversation" && doc.Owner == conversation.ID && !doc.Retired {
					kinds[doc.Kind] = true
				}
			}
			for _, kind := range []string{"pi.agent", "pi.live", "pi.inbox", "pi.usage"} {
				if !kinds[kind] {
					t.Error("missing created builtin", kind)
				}
			}
			agent, _ := agentDocument(candidate, conversation.ID)
			cwd, _ := agent.Value["cwd"].(string)
			label := "new"
			if conversation.Parent != 0 {
				label = "fork"
			}
			seen = append(seen, label+":"+cwd)
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			if _, err := tx.CreateDocument(Document{ID: id, Scope: "conversation", Owner: conversation.ID, Kind: "app.created", Version: 1, Value: JSON{"created": true}}); err != nil {
				return err
			}
			if panicNext {
				panic("private creation panic")
			}
			if failure {
				return errors.New("creation failed")
			}
			return nil
		}}
		h := openHarness(t, b.store, options)
		root, err := h.Root(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}, Cwd: "/root"})
		if err != nil {
			t.Fatal(err)
		}
		independent, err := h.CreateConversation(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}, Cwd: "/independent"})
		if err != nil {
			t.Fatal(err)
		}
		agent, err := independent.Agent(bg)
		if err != nil || agent.Configuration.Cwd != "/independent" {
			t.Fatal(agent, err)
		}
		var plain, owned, at ID
		definition := taskDefinition(t, "task.creation.owner", func(context.Context, TaskRecord, *TaskRuntime) error { return nil })
		_, err = h.CommitTasks(bg, root.ID(), func(tx *Tx) error {
			owner, err := tx.CreateTask(definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
			if err != nil {
				return err
			}
			plain, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.CreateConversation(Conversation{ID: plain}); err != nil {
				return err
			}
			owned, err = tx.MintID()
			if err != nil {
				return err
			}
			if err := tx.CreateConversation(Conversation{ID: owned, Owner: owner}); err != nil {
				return err
			}
			candidate, err := tx.current()
			if err != nil {
				return err
			}
			doc, ok := agentDocument(candidate, owned)
			if !ok || doc.Value["cwd"] != "/root" {
				t.Error("owned agent not copied", doc)
			}
			at, err = tx.MintID()
			if err != nil {
				return err
			}
			return tx.AppendEntry(Entry{ID: at, Conversation: root.ID(), Kind: "app.cut", Value: JSON{}})
		})
		if err != nil {
			t.Fatal(err)
		}
		fork, err := root.Fork(bg, at)
		if err != nil {
			t.Fatal(err)
		}
		value, err := fork.Agent(bg)
		if err != nil || value.Configuration.Cwd != "/root" {
			t.Fatal(value, err)
		}
		if strings.Join(seen, "|") != "new:|new:|new:|new:/root|fork:/root" {
			t.Fatal("creation ordering", seen)
		}
		for _, panicCase := range []bool{false, true} {
			failure = !panicCase
			panicNext = panicCase
			before, _ := h.Snapshot(bg)
			_, err = h.CreateConversation(bg, AgentChange{Model: ModelRef{Provider: "openai", ID: "model"}})
			if err == nil {
				t.Fatal("creation failure hidden")
			}
			after, _ := h.Snapshot(bg)
			if after.Seq != before.Seq || len(after.Conversations) != len(before.Conversations) || len(after.Documents) != len(before.Documents) {
				t.Fatal("creation failure leaked", before.Seq, after.Seq)
			}
			// Catching the callback failure cannot admit other transaction writes.
			_, err = h.CommitTasks(bg, root.ID(), func(tx *Tx) error {
				id, err := tx.MintID()
				if err != nil {
					return err
				}
				_ = tx.CreateConversation(Conversation{ID: id})
				entry, err := tx.MintID()
				if err != nil {
					return err
				}
				return tx.AppendEntry(Entry{ID: entry, Conversation: root.ID(), Kind: "app.must-rollback", Value: JSON{}})
			})
			if err == nil {
				t.Fatal("caught creation failure committed")
			}
			after, _ = h.Snapshot(bg)
			if after.Seq != before.Seq || len(after.Entries) != len(before.Entries) {
				t.Fatal("creation error transaction not atomic")
			}
			if _, err := root.Fork(bg, at); err == nil {
				t.Fatal("fork callback failure accepted")
			}
			after, _ = h.Snapshot(bg)
			if after.Seq != before.Seq || len(after.Conversations) != len(before.Conversations) {
				t.Fatal("failed fork leaked")
			}
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		count := len(seen)
		second := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		state, err := second.Snapshot(bg)
		if err != nil || len(seen) != count {
			t.Fatal("Open replayed creation callback", err, len(seen), count)
		}
		if _, ok := state.Conversations[fork.ID()]; !ok {
			t.Fatal("successful fork lost")
		}
	})
}
