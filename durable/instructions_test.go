package durable

import (
	"context"
	"fmt"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
)

func TestAgentInstructionsReservedSectionDetachedExplicitEmptyAndClear(t *testing.T) {
	for _, withExtensions := range []bool{false, true} {
		t.Run(fmt.Sprintf("extensions-%t", withExtensions), func(t *testing.T) {
			backends(t, func(t *testing.T, b backend) {
				var seen []string
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					seen = append(seen, input.SystemPrompt)
					ch := make(chan goai.Event, 1)
					ch <- terminal("answer")
					close(ch)
					return ch
				})
				registry := NewRegistry()
				if withExtensions {
					if err := registry.Install(&Extension{Name: "base", Sections: []PromptSection{{Key: "base", Render: func(context.Context, PromptInput) (*string, error) { text := "base text"; return &text, nil }}, {Key: "other", Render: func(context.Context, PromptInput) (*string, error) {
						text := "other text"
						return &text, nil
					}}}}); err != nil {
						t.Fatal(err)
					}
				}
				options.Registry = registry
				h := openHarness(t, b.store, options)
				instructions := "agent instructions"
				root, err := h.Root(bg, AgentChange{Model: ref, Instructions: &instructions})
				if err != nil {
					t.Fatal(err)
				}
				instructions = "mutated"
				for step := 0; step < 3; step++ {
					if step == 1 {
						empty := ""
						if err := root.ConfigurePatch(bg, AgentPatch{Instructions: &empty}); err != nil {
							t.Fatal(err)
						}
					}
					if step == 2 {
						if err := root.ConfigurePatch(bg, AgentPatch{Clear: []string{"instructions"}}); err != nil {
							t.Fatal(err)
						}
					}
					sub, err := root.Submit(bg, Input{Content: "question"})
					if err != nil {
						t.Fatal(err)
					}
					if result := waitSubmission(t, sub); result.Submission.Status != "done" {
						t.Fatal(result)
					}
				}
				if len(seen) != 3 {
					t.Fatal(seen)
				}
				if !strings.HasSuffix(seen[0], "<instructions>\nagent instructions\n</instructions>") || strings.Contains(seen[0], "extension instructions") {
					t.Fatal("reserved instruction override", seen)
				}
				if !strings.HasSuffix(seen[1], "<instructions>\n\n</instructions>") {
					t.Fatal("empty instructions dropped", seen[1])
				}
				if strings.Contains(seen[2], "<instructions>") {
					t.Fatal("clear retained instructions", seen[2])
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				patches := 0
				for _, entry := range state.Entries {
					if entry.Kind == "pi.system" {
						for _, message := range entry.Model {
							if _, exists := message.Sections["instructions"]; exists {
								patches++
							}
						}
					}
				}
				if patches < 3 {
					t.Fatal("instructions not persisted in prompt transcript", patches)
				}
			})
		})
	}
}
