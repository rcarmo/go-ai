package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120ExtensionNamespacesCapturedForPromptAndVM(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		registry := NewRegistry()
		if err = registry.Register(wrapRegistration("ns_leaf")); err != nil {
			t.Fatal(err)
		}
		namespaces := []CodemodeNamespace{{Name: "extension", Prefix: "ns_", Description: "original"}}
		extension, err := NewCodemodeExtension(engine, CodemodeExtensionOptions{Namespaces: &namespaces})
		if err != nil {
			t.Fatal(err)
		}
		namespaces[0].Description = "mutated"
		if err = registry.Install(&extension); err != nil {
			t.Fatal(err)
		}
		requests := 0
		ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, input *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			requests++
			ch := make(chan goai.Event, 1)
			if !strings.Contains(goai.ResolveContext(input, false).SystemPrompt, "## extension\noriginal") || strings.Contains(goai.ResolveContext(input, false).SystemPrompt, "## harness") {
				t.Error("namespace prompt drift", goai.ResolveContext(input, false).SystemPrompt)
			}
			if requests == 1 {
				message := &goai.Message{Role: goai.RoleAssistant, StopReason: goai.StopReasonToolUse, Content: []goai.ContentBlock{{Type: "toolCall", ID: "ns-call", Name: "codemode", Arguments: map[string]any{"code": `const ns=await describeNamespace("extension");if(ns.description!=="original")throw Error("namespace drift");text(await tools.ns_leaf({}));`}}}}
				ch <- &goai.DoneEvent{Reason: goai.StopReasonToolUse, Message: message}
			} else {
				last := input.Messages[len(input.Messages)-1]
				if last.IsError || !strings.HasSuffix(goai.ContentText(last.Content), "Output:\nbase") {
					t.Error(last)
				}
				ch <- terminal("done")
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		options.CodemodeNamespaces = []CodemodeNamespace{{Name: "harness", Prefix: "ns_", Description: "wrong"}}
		h := openHarness(t, b.store, options)
		conv := root(t, h, ref)
		sub, err := conv.Submit(bg, Input{Content: "run"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 5*time.Second)
		defer cancel()
		result, err := sub.Wait(ctx)
		if err != nil || result.Submission.Status != "done" || requests != 2 {
			t.Fatal(result, err, requests)
		}
	})
	invalid := []CodemodeNamespace{{Name: "bad", Prefix: ""}}
	if _, err = NewCodemodeExtension(engine, CodemodeExtensionOptions{Namespaces: &invalid}); err == nil {
		t.Fatal("invalid namespace accepted")
	}
}

// Source: codemode.test.ts, extension namespaces need not match tool prefixes,
// and empty namespaces are visible both in the prompt and in the guest.
func TestSourceV160CodemodeExtensionNamespaceDiscovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	engine, err := NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	registry := NewRegistry()
	if err := registry.Install(&Extension{Name: "ungrouped", Tools: []ToolRegistration{wrapRegistration("echo")}}); err != nil {
		t.Fatal(err)
	}
	github := &ToolNamespace{Name: "github", Description: "GitHub MCP server", Instructions: "Pass the repository as owner/name."}
	if err := registry.Install(&Extension{Name: "github-extension", Namespace: github, Tools: []ToolRegistration{wrapRegistration("list_issues"), wrapRegistration("echo")}}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&Extension{Name: "github-second", Namespace: &ToolNamespace{Name: "github", Description: "second declaration"}}); err != nil {
		t.Fatal(err)
	}
	github.Description = "mutated after install"
	if err := registry.Install(&Extension{Name: "linear-extension", Namespace: &ToolNamespace{Name: "linear", Description: "Linear: sign in first."}}); err != nil {
		t.Fatal(err)
	}
	codemode, err := NewCodemodeExtension(engine)
	if err != nil {
		t.Fatal(err)
	}
	if err := registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	const script = `const found = await searchTools("issues");
const byInstructions = await searchTools("repository owner");
const inNamespace = await searchTools("echo", { namespace: "github" });
return {
  found: found.map(tool => tool.name),
  byInstructions: byInstructions.map(tool => tool.name),
  inNamespace,
  described: (await describeTool("echo")).includes("echo(args:"),
  missing: (await describeTool("nope")) ?? null,
  namespace: await describeNamespace("github"),
  empty: await describeNamespace("linear"),
  all: ALL_TOOLS.map(tool => tool.name).includes("list_issues")
};`
	var providers atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if providers.Add(1) == 1 {
			ch <- toolAnswer("ns", "codemode", JSON{"code": script})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	native, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, native, options)
	conversation := root(t, h, ref)
	sub, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := sub.Wait(ctx); err != nil || result.Record.Status != "done" {
		t.Fatalf("submission = %+v, %v", result.Record, err)
	}
	result := v160ToolResult(t, ctx, conversation, "ns")
	body := v160ToolResultText(result)
	_, output, ok := strings.Cut(body, "Output:\n")
	if !ok || result.IsError {
		t.Fatalf("codemode result = %q", body)
	}
	var got, want any
	const expected = `{"found":["list_issues"],"byInstructions":["list_issues"],"inNamespace":[],"described":true,"missing":null,"namespace":{"name":"github","description":"GitHub MCP server","instructions":"Pass the repository as owner/name.","tools":["list_issues"]},"empty":{"name":"linear","description":"Linear: sign in first.","tools":[]},"all":true}`
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("guest output = %s, %v; want %s", output, err, expected)
	}
	page, err := conversation.Entries(ctx, EntryQuery{}, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	var prompt string
	for _, entry := range page.Items {
		for _, message := range entry.Model {
			if message.Role == goai.RoleSystem && message.Sections["codemode"] != nil {
				prompt = *message.Sections["codemode"]
			}
		}
	}
	if !strings.Contains(prompt, "## github\nGitHub MCP server") || !strings.Contains(prompt, "## linear\nLinear: sign in first.") || strings.Contains(prompt, "mutated after install") || strings.Contains(prompt, "second declaration") || !strings.Contains(prompt, "### `echo`") || strings.Index(prompt, "### `echo`") > strings.Index(prompt, "## github") {
		t.Fatalf("codemode prompt = %q", prompt)
	}
}
