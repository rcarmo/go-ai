package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"sync/atomic"
	"testing"
)

func promptString(text string) *string { return &text }
func TestPromptSectionReplayPlanOrderAndFailure(t *testing.T) {
	messages := []MessageReceipt{{Role: goai.RoleSystem, Sections: map[string]*string{"a": promptString("A"), "b": promptString("B")}, SectionOrder: []string{"b", "a"}}}
	shown := ReplayPromptSections(messages)
	if len(shown) != 2 || shown[0].Key != "b" {
		t.Fatal(shown)
	}
	desired := []RenderedSection{{"a", "A"}, {"b", "B"}}
	entries, err := PlanPromptEntries(ContextView{Messages: messages}, desired, 10)
	if err != nil || len(entries) != 2 {
		t.Fatal(entries, err)
	}
	for _, entry := range entries {
		messages = append(messages, entry.Model...)
	}
	replayed := ReplayPromptSections(messages)
	if len(replayed) != 2 || replayed[0] != desired[0] || replayed[1] != desired[1] {
		t.Fatal(replayed)
	}
	entries, err = PlanPromptEntries(ContextView{Messages: messages}, desired, 11)
	if err != nil || len(entries) != 0 {
		t.Fatal("unchanged prompt wrote", entries, err)
	}
	reportCount := 0
	sections := []PromptSection{{Key: "a", Render: func(context.Context, PromptInput) (*string, error) { panic("private") }}, {Key: "c", Render: func(context.Context, PromptInput) (*string, error) { return promptString("C"), nil }}}
	rendered, err := RenderPromptSections(bg, sections, PromptInput{}, desired, func(error) { reportCount++ })
	if err != nil || reportCount != 1 || len(rendered) != 2 || rendered[0] != desired[0] || rendered[1].Text != "<c>\nC\n</c>" {
		t.Fatal(rendered, err, reportCount)
	}
	caller, cancel := context.WithCancel(bg)
	cancel()
	if _, err := RenderPromptSections(caller, sections, PromptInput{}, nil, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	head := Entry{ID: 4, Head: 4}
	baseline, err := PlanPromptEntries(ContextView{Head: &head, Entries: []Entry{{ID: 2, Kind: "pi.system"}}, Messages: messages}, desired, 12)
	if err != nil || len(baseline) != 1 || len(baseline[0].Edits) != 1 || baseline[0].Edits[0].Target != 2 {
		t.Fatal(baseline, err)
	}
}
func TestPromptSectionsPersistProductionRequestAndKeepTextOnError(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var renders, requests, reports atomic.Int64
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			requests.Add(1)
			if !strings.Contains(input.SystemPrompt, "<policy>\nuse tools safely\n</policy>") {
				t.Error("rendered prompt missing", input.SystemPrompt)
			}
			ch := make(chan goai.Event, 1)
			ch <- terminal("ok")
			close(ch)
			return ch
		})
		options.OnReport = func(error) { reports.Add(1) }
		sections := []PromptSection{{Key: "policy", Render: func(context.Context, PromptInput) (*string, error) {
			if renders.Add(1) > 1 {
				return nil, errors.New("render failure")
			}
			return promptString("use tools safely"), nil
		}}}
		options.Registry = NewRegistry()
		if err := options.Registry.Install(&Extension{Name: "policy-extension", Sections: sections}); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, options)
		conversation := root(t, h, ref)
		for index := 0; index < 2; index++ {
			sub, err := conversation.Submit(bg, Input{Content: "next"})
			if err != nil {
				t.Fatal(err)
			}
			if result := waitSubmission(t, sub); result.Submission.Status != "done" {
				t.Fatal(result)
			}
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		systems := 0
		for _, entry := range state.Entries {
			if entry.Kind == "pi.system" {
				systems++
				if len(entry.Model) != 1 || len(entry.Model[0].SectionOrder) != 1 {
					t.Fatal("prompt order not persisted", entry)
				}
			}
		}
		if systems != 1 || requests.Load() != 2 || renders.Load() != 2 || reports.Load() != 1 {
			t.Fatal("prompt rewrite/error behavior", systems, requests.Load(), renders.Load(), reports.Load())
		}
	})
}
