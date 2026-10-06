package durable

import (
	"errors"
	"testing"
)

func TestRootWithInitAtomicOnceAfterAgentAndCreationRollback(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		calls := 0
		hookCalls := 0
		h := openHarness(t, b.store, Options{ConversationCreated: func(*Tx, Conversation) error { hookCalls++; return nil }})
		fail := true
		init := func(tx *Tx, id ID) error {
			calls++
			candidate, err := tx.current()
			if err != nil {
				return err
			}
			agent, ok := agentDocument(candidate, id)
			if !ok || agent.Value["cwd"] != "/root" {
				t.Fatal("init before agent", agent)
			}
			entry, err := tx.MintID()
			if err != nil {
				return err
			}
			if err = tx.AppendEntry(Entry{ID: entry, Conversation: id, Kind: "note", Value: JSON{}}); err != nil {
				return err
			}
			if fail {
				return errors.New("init failed")
			}
			return nil
		}
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := h.RootWithInit(bg, AgentChange{Cwd: "/root"}, init); err == nil {
			t.Fatal("failed root init admitted")
		}
		after, err := h.Snapshot(bg)
		if err != nil || before.Seq != after.Seq || len(after.Documents) != len(before.Documents) || len(after.Entries) != len(before.Entries) {
			t.Fatal("failed root leaked", after, err)
		}
		fail = false
		root, err := h.RootWithInit(bg, AgentChange{Cwd: "/root"}, init)
		if err != nil {
			t.Fatal(err)
		}
		ready, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		again, err := h.RootWithInit(bg, AgentChange{Cwd: "/ignored"}, func(*Tx, ID) error { t.Fatal("existing root initializer repeated"); return nil })
		if err != nil || again.ID() != root.ID() {
			t.Fatal(again, err)
		}
		final, err := h.Snapshot(bg)
		if err != nil || final.Seq != ready.Seq || calls != 2 || hookCalls != 2 {
			t.Fatal("root not once", calls, hookCalls, ready.Seq, final.Seq, err)
		}
		agent, err := root.Agent(bg)
		if err != nil || agent.Configuration.Cwd != "/root" {
			t.Fatal(agent, err)
		}
	})
}
