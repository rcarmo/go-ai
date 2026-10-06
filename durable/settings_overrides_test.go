package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestConversationSettingsPartialOverridePresenceLiveDefaultsAndReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		zero, disabled := 0, false
		delay := int64(321)
		h := taskTestHarnessOptions(t, b.store, Options{Settings: &HarnessSettings{Retry: &RetrySettings{BaseDelayMs: &delay}}})
		stored := RequestSettings{RetryOverrides: &RetrySettings{Enabled: &disabled}, CompactionOverrides: &CompactionSettings{KeepRecentTokens: &zero}}
		conversation, err := h.CreateConversation(bg, AgentChange{Settings: stored})
		if err != nil {
			t.Fatal(err)
		}
		disabled = true
		zero = 999
		agent, err := conversation.Agent(bg)
		if err != nil {
			t.Fatal(err)
		}
		resolved := agent.Configuration.Settings
		if resolved.Retry.Enabled || resolved.Retry.BaseDelayMs != 321 || resolved.Retry.MaxRetries != 3 || resolved.Compaction.KeepRecentTokens != 0 || resolved.Compaction.ReserveTokens != 16384 || !resolved.Compaction.Enabled || resolved.RetryOverrides != nil {
			t.Fatal("per-field presence", resolved)
		}
		nextDelay := int64(777)
		if err := h.SetSettings(bg, HarnessSettings{Retry: &RetrySettings{BaseDelayMs: &nextDelay}}); err != nil {
			t.Fatal(err)
		}
		agent, err = conversation.Agent(bg)
		if err != nil || agent.Configuration.Settings.Retry.Enabled || agent.Configuration.Settings.Retry.BaseDelayMs != 777 || agent.Configuration.Settings.Compaction.KeepRecentTokens != 0 {
			t.Fatal("pinned override hid live defaults", agent, err)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		h = taskTestHarnessOptions(t, reopenStoreAfterHarnessClose(t, b.store), Options{Settings: &HarnessSettings{Retry: &RetrySettings{BaseDelayMs: &nextDelay}}})
		conversation, err = h.Conversation(bg, conversation.ID())
		if err != nil {
			t.Fatal(err)
		}
		agent, err = conversation.Agent(bg)
		if err != nil || agent.Configuration.Settings.Retry.Enabled || agent.Configuration.Settings.Compaction.KeepRecentTokens != 0 || agent.Configuration.Settings.Retry.BaseDelayMs != 777 {
			t.Fatal("override lost reopen", agent, err)
		}
		negative := -1
		before, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		invalid := RequestSettings{CompactionOverrides: &CompactionSettings{KeepRecentTokens: &negative}}
		if err := conversation.ConfigurePatch(bg, AgentPatch{Settings: &invalid}); err == nil {
			t.Fatal("negative explicit override accepted")
		}
		after, err := h.Snapshot(bg)
		if err != nil || after.Seq != before.Seq {
			t.Fatal("invalid override wrote", err)
		}
	})
}

func TestConversationPartialRetryDisabledKeepsDefaultsAtActualDispatch(t *testing.T) {
	calls := 0
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls++
		ch := make(chan goai.Event, 1)
		ch <- &goai.ErrorEvent{Reason: goai.StopReasonError, Error: &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonError, ErrorMessage: "503 overloaded", Content: []goai.ContentBlock{}}}
		close(ch)
		return ch
	})
	h := openHarness(t, mustMemory(t), options)
	disabled := false
	root, err := h.Root(bg, AgentChange{Model: ref, Settings: RequestSettings{RetryOverrides: &RetrySettings{Enabled: &disabled}}})
	if err != nil {
		t.Fatal(err)
	}
	sub, err := root.Submit(bg, Input{Content: "question"})
	if err != nil {
		t.Fatal(err)
	}
	if result := waitSubmission(t, sub); result.Submission.Status != "failed" || calls != 1 {
		t.Fatal(result, calls)
	}
}
