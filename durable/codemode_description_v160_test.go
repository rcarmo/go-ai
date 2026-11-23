package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// codemode-describe.test.ts:41–48. Read the description that reached the
// public model provider, not a copy of the formatter in the fixture. The
// guest separately confirms that the `models` global is present only when
// the description is allowed to advertise it.
func TestSourceV160CodemodeConstantDescription(t *testing.T) {
	allow, deny := true, false
	for _, tc := range []struct {
		name       string
		paid       bool
		models     *bool
		wantModels bool
		portable   bool
	}{
		{name: "default"},
		{name: "paid", paid: true, wantModels: true},
		{name: "extension-restricts-host", paid: true, models: &deny},
		{name: "extension-cannot-grant", models: &allow},
		{name: "portable-without-model-globals", paid: true, portable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var sandbox CodemodeSandbox
			if tc.portable {
				sandbox, _ = newJournalWorkerSandboxV160(t)
			} else {
				engine, err := NewQuickJSSandbox(ctx)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = engine.Close(context.Background()) })
				sandbox = engine
			}
			registry := NewRegistry()
			extension, err := NewCodemodeExtension(sandbox, CodemodeExtensionOptions{Models: tc.models})
			if err != nil {
				t.Fatal(err)
			}
			if err := registry.Install(&extension); err != nil {
				t.Fatal(err)
			}
			descriptions := make(chan string, 3)
			var requests atomic.Int32
			ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
				n := requests.Add(1)
				description := ""
				leaf := false
				for _, tool := range input.Tools {
					if tool.Name == "codemode" {
						description = tool.Description
					}
					leaf = leaf || tool.Name == "new_leaf"
				}
				if leaf != (n == 3) {
					t.Errorf("provider request %d: new_leaf present=%t", n, leaf)
				}
				if n <= 3 {
					descriptions <- description
				}
				ch := make(chan goai.Event, 1)
				if n == 1 {
					ch <- toolAnswer("s1", "codemode", JSON{"code": `return typeof models;`})
				} else {
					ch <- terminal("done")
				}
				close(ch)
				return ch
			})
			options.Registry, options.PaidModels = registry, tc.paid
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, options)
			conversation := root(t, h, ref)
			sub, err := conversation.Submit(ctx, Input{Content: "show"})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			if settlement, err := sub.Wait(ctx); err != nil || settlement.Submission.Status != "done" || requests.Load() != 2 {
				t.Fatalf("submission = %+v, err=%v, requests=%d", settlement, err, requests.Load())
			}
			description, next := <-descriptions, <-descriptions
			for _, want := range []string{"`await tools.<name>({ ...args })`", "searchTools(query", "The `codemode` section of the system prompt lists the tools scripts can call"} {
				if !strings.Contains(description, want) {
					t.Errorf("provider description lacks %q: %q", want, description)
				}
			}
			if got := strings.Contains(description, "`models`"); got != tc.wantModels {
				t.Errorf("provider described `models`=%t; want %t", got, tc.wantModels)
			}
			if description != next {
				t.Error("provider changed the Codemode description between turns")
			}
			result := v160ToolResult(t, ctx, conversation, "s1")
			_, value, ok := strings.Cut(v160ToolResultText(result), "\nOutput:\n")
			want := "undefined"
			if tc.wantModels {
				want = "object"
			}
			if result.IsError || !ok || value != want {
				t.Errorf("guest typeof models = %q, error=%t; want %q", v160ToolResultText(result), result.IsError, want)
			}
			// The catalogue can change without changing the tool description.
			if err := registry.Register(wrapRegistration("new_leaf")); err != nil {
				t.Fatal(err)
			}
			another, err := h.CreateConversation(ctx, AgentChange{Model: ref, SystemPrompt: "different conversation"})
			if err != nil {
				t.Fatal(err)
			}
			sub, err = another.Submit(ctx, Input{Content: "show"})
			if err != nil {
				t.Fatal(err)
			}
			if settlement, err := sub.Wait(ctx); err != nil || settlement.Submission.Status != "done" || requests.Load() != 3 {
				t.Fatalf("second conversation = %+v, err=%v, requests=%d", settlement, err, requests.Load())
			}
			if got := <-descriptions; got != description {
				t.Error("provider changed the Codemode description for another conversation after the catalogue grew")
			}
		})
	}
}
