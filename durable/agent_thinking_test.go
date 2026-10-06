package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"testing"
)

func TestAgentThinkingPinnedLevelsClearRequestForkAndReopen(t *testing.T) {
	expected := goai.ModelThinkingLevel("off")
	requests := 0
	ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, _ *goai.Context, request *goai.StreamOptions) <-chan goai.Event {
		requests++
		if expected == "off" {
			if request.Reasoning != nil {
				t.Error("off reasoning forwarded", request.Reasoning)
			}
		} else if request.Reasoning == nil || string(*request.Reasoning) != string(expected) {
			t.Error("thinking not forwarded", request.Reasoning, expected)
		}
		ch := make(chan goai.Event, 1)
		ch <- terminal("done")
		close(ch)
		return ch
	})
	store, dir := newJournal(t)
	h := openHarness(t, store, options)
	conv := root(t, h, ref)
	view, err := conv.Agent(bg)
	if err != nil || view.Configuration.ThinkingLevel != "off" {
		t.Fatal(view, err)
	}
	var at ID
	for _, level := range []goai.ModelThinkingLevel{"minimal", "low", "medium", "high", "xhigh", "off"} {
		expected = level
		if err := conv.ConfigurePatch(bg, AgentPatch{ThinkingLevel: &level}); err != nil {
			t.Fatal(err)
		}
		sub, err := conv.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		waitSubmission(t, sub)
		if level == "high" {
			snapshot, _ := h.Snapshot(bg)
			for _, entry := range snapshot.Entries {
				if entry.Conversation == conv.ID() && entry.Kind == "message" && entry.ID > at {
					at = entry.ID
				}
			}
		}
	}
	before, _ := h.Snapshot(bg)
	invalid := goai.ModelThinkingLevel("invalid")
	if err := conv.ConfigurePatch(bg, AgentPatch{ThinkingLevel: &invalid}); err == nil {
		t.Fatal("invalid level accepted")
	}
	after, _ := h.Snapshot(bg)
	if after.Seq != before.Seq {
		t.Fatal("invalid thinking wrote")
	}
	if err := conv.ConfigurePatch(bg, AgentPatch{ThinkingLevel: &invalid, Clear: []string{"thinkingLevel"}}); err == nil {
		t.Fatal("conflicting thinking accepted")
	}
	if err := conv.ConfigurePatch(bg, AgentPatch{Clear: []string{"thinkingLevel"}}); err != nil {
		t.Fatal(err)
	}
	view, err = conv.Agent(bg)
	if err != nil || view.Configuration.ThinkingLevel != "off" {
		t.Fatal(view, err)
	}
	fork, err := conv.Fork(bg, at)
	if err != nil {
		t.Fatal(err)
	}
	view, err = fork.Agent(bg)
	if err != nil || view.Configuration.ThinkingLevel != "high" {
		t.Fatal("historical thinking", view, err)
	}
	if err := h.Close(bg); err != nil {
		t.Fatal(err)
	}
	store, err = OpenJournal(dir, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	h = openHarness(t, store, options)
	fork, err = h.Conversation(bg, fork.ID())
	if err != nil {
		t.Fatal(err)
	}
	view, err = fork.Agent(bg)
	if err != nil || view.Configuration.ThinkingLevel != "high" || requests != 6 {
		t.Fatal("thinking reopen", view, err, requests)
	}
}
