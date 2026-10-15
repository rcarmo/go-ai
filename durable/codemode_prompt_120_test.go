package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
	"strings"
	"testing"
	"time"
)

func TestCodemode120PromptPersistsReopensAndTracksSelection(t *testing.T) {
	for _, parallel := range []bool{false, true} {
		t.Run(map[bool]string{false: "serial", true: "parallel"}[parallel], func(t *testing.T) { testCodemodePrompt120(t, parallel) })
	}
}
func testCodemodePrompt120(t *testing.T, parallel bool) {
	sandbox, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer sandbox.Close(bg)
	sandbox.ParallelCalls = parallel
	guidance := "serially"
	if parallel {
		guidance = "Promise.all"
	}
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		for _, name := range []string{"script_visible", "model_secret", "deferred_secret"} {
			tool := wrapRegistration(name)
			if name == "model_secret" {
				tool.Callers = []string{"model"}
			}
			if name == "deferred_secret" {
				tool.Deferred = true
			}
			if err := registry.Register(tool); err != nil {
				t.Fatal(err)
			}
		}
		extension, err := NewCodemodeExtension(sandbox)
		if err != nil {
			t.Fatal(err)
		}
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		enabled := true
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			prompt := input.SystemPrompt
			if enabled {
				if !strings.Contains(prompt, "script_visible") || !strings.Contains(prompt, "searchTools") || !strings.Contains(prompt, guidance) {
					t.Error("codemode prompt missing", prompt)
				}
				if strings.Contains(prompt, "deferred_secret") {
					t.Error("deferred tool leaked inline", prompt)
				}
				if strings.Contains(prompt, "model_secret") {
					t.Error("model-only tool leaked", prompt)
				}
			} else if strings.Contains(prompt, guidance) {
				t.Error("deselected codemode prompt retained", prompt)
			}
			ch := make(chan goai.Event, 1)
			ch <- terminal("done")
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		run := func(c *ConversationHandle) {
			t.Helper()
			sub, err := c.Submit(bg, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(bg, 5*time.Second)
			defer cancel()
			result, err := sub.Wait(ctx)
			if err != nil || result.Submission.Status != "done" {
				t.Fatal(result, err)
			}
		}
		run(conv)
		before, err := conv.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		sections := ReplayPromptSections(before.Messages)
		if len(sections) != 1 || sections[0].Key != "codemode" {
			t.Fatal("persisted section", sections)
		}
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), options)
		reopened, err := next.Conversation(bg, conv.ID())
		if err != nil {
			t.Fatal(err)
		}
		run(reopened)
		after, err := reopened.ContextView(bg, 0)
		if err != nil {
			t.Fatal(err)
		}
		count := func(view ContextView) int {
			n := 0
			for _, entry := range view.Entries {
				if entry.Kind == "pi.system" {
					n++
				}
			}
			return n
		}
		if count(before) != count(after) {
			t.Fatal("reopen duplicated prompt", count(before), count(after))
		}
		none := []string{}
		if err = reopened.Configure(bg, AgentChange{Model: ref, Tools: &none}); err != nil {
			t.Fatal(err)
		}
		enabled = false
		run(reopened)
		after, err = reopened.ContextView(bg, 0)
		if err != nil || len(ReplayPromptSections(after.Messages)) != 0 {
			t.Fatal("deselected section persists", err, ReplayPromptSections(after.Messages))
		}
	})
}
