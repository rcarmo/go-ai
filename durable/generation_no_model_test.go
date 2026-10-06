package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestUnconfiguredConversationNoModelFailsWithoutSyntheticAssistantAndCanConfigure(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		calls := 0
		ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
			calls++
			ch := make(chan goai.Event, 1)
			ch <- terminal("answer")
			close(ch)
			return ch
		})
		h := openHarness(t, b.store, options)
		conversation, err := h.CreateConversation(bg, AgentChange{})
		if err != nil {
			t.Fatal(err)
		}
		sub, err := conversation.Submit(bg, Input{Content: "unconfigured"})
		if err != nil {
			t.Fatal(err)
		}
		result := waitSubmission(t, sub)
		if result.Submission.Status != "failed" || result.Message != nil || result.Submission.Value["errorCode"] != "no_model" || calls != 0 {
			t.Fatal(result, calls)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		for _, task := range state.Tasks {
			record, err := CanonicalTask(task, h.session.limits)
			if err != nil || record.State.Outcome.Status != "failed" || record.State.Outcome.Error.Message != "No model is configured" {
				t.Fatal(record, err)
			}
		}
		users := 0
		for _, entry := range state.Entries {
			if entry.Value["role"] == string(goai.RoleUser) {
				users++
			}
			if entry.Value["role"] == string(goai.RoleAssistant) {
				t.Fatal("no-model invented assistant", entry)
			}
		}
		if users != 1 {
			t.Fatal("admitted input missing", users)
		}
		if err := conversation.ConfigurePatch(bg, AgentPatch{Model: &ref}); err != nil {
			t.Fatal(err)
		}
		sub, err = conversation.Submit(bg, Input{Content: "configured"})
		if err != nil {
			t.Fatal(err)
		}
		if result := waitSubmission(t, sub); result.Submission.Status != "done" || calls != 1 {
			t.Fatal(result, calls)
		}
		if err := conversation.ConfigurePatch(bg, AgentPatch{Clear: []string{"model"}}); err != nil {
			t.Fatal(err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		conversation, err = h.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		sub, err = conversation.Submit(bg, Input{Content: "cleared"})
		if err != nil {
			t.Fatal(err)
		}
		if result := waitSubmission(t, sub); result.Submission.Status != "failed" || result.Message != nil || calls != 1 {
			t.Fatal(result, calls)
		}
	})
}
