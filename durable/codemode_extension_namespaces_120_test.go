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

// Supplemental composition of the pinned Codemode discovery, restart, and
// journaled-global cases. No upstream original changes installed declarations
// between these observations of a single script.
func TestV160CodemodeExtensionNamespaceReplay(t *testing.T) {
	ctx, cancel := context.WithTimeout(bg, 12*time.Second)
	defer cancel()
	engine, err := NewQuickJSSandbox(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	// The first two deliveries are confirmed when the guest issues call 3.
	sandbox := &revokeSandbox120{sandbox: engine, ready: make(chan struct{}), holdCall: 3}
	registry := NewRegistry()
	var retired, moved atomic.Int32
	tool := func(name, value string, count *atomic.Int32) ToolRegistration {
		registration := wrapRegistration(name)
		registration.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
			count.Add(1)
			return ToolResult{Content: value}, nil
		}
		return registration
	}
	post := tool("post_issue", "posted", &moved)
	if err = registry.Install(&Extension{
		Name:      "issue-tracker",
		Namespace: &ToolNamespace{Name: "github", Description: "GitHub server", Instructions: "Use owner/name"},
		Tools:     []ToolRegistration{tool("list_issues", "confirmed", &retired), post},
	}); err != nil {
		t.Fatal(err)
	}
	codemode, err := NewCodemodeExtension(sandbox)
	if err != nil {
		t.Fatal(err)
	}
	if err = registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	const script = `const before = await describeNamespace("github");
const confirmed = await tools.list_issues({});
const after = await describeNamespace("github");
const search = (await searchTools("post_issue", {namespace: "github", limit: 1})).map(tool => tool.name);
const replacement = await describeNamespace("linear");
const posted = await tools.post_issue({});
let denial;
try { await tools.list_issues({}); } catch (error) { denial = error.message; }
return {before, confirmed, after, search, replacement: replacement ?? null, posted, denial,
  listed: ALL_TOOLS.some(tool => tool.name === "list_issues"),
  newTool: ALL_TOOLS.some(tool => tool.name === "assign_issue")};`
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if requests.Add(1) == 1 {
			ch <- toolAnswer("namespace-replay", "codemode", JSON{"code": script})
		} else {
			ch <- terminal("done")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	h := openHarness(t, store, options)
	conversation := root(t, h, ref)
	submission, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, sandbox.ready)
	if retired.Load() != 1 || moved.Load() != 0 {
		t.Fatalf("initial effects: list=%d post=%d", retired.Load(), moved.Load())
	}
	if err := h.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err = registry.Install(&Extension{
		Name:      "issue-tracker",
		Namespace: &ToolNamespace{Name: "linear", Description: "Replacement tracker"},
		Tools:     []ToolRegistration{post, wrapRegistration("assign_issue")},
	}); err != nil {
		t.Fatal(err)
	}
	next := openHarness(t, reopenStoreAfterHarnessClose(t, store), options)
	if err := next.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	reopened, err := next.Submission(ctx, submission.ID())
	if err != nil || reopened == nil {
		t.Fatal(reopened, err)
	}
	if result, err := reopened.Wait(ctx); err != nil || result.Submission.Status != "done" {
		t.Fatalf("reopened submission = %+v, %v", result, err)
	}
	conversation, err = next.Conversation(ctx, conversation.ID())
	if err != nil {
		t.Fatal(err)
	}
	result := v160ToolResult(t, ctx, conversation, "namespace-replay")
	body := v160ToolResultText(result)
	_, output, ok := strings.Cut(body, "Output:\n")
	if !ok || result.IsError {
		t.Fatalf("codemode replay result = %q", body)
	}
	const namespace = `{"name":"github","description":"GitHub server","instructions":"Use owner/name","tools":["list_issues","post_issue"]}`
	const expected = `{"before":` + namespace + `,"confirmed":"confirmed","after":` + namespace + `,"search":["post_issue"],"replacement":null,"posted":"posted","denial":"Tool list_issues is no longer callable","listed":true,"newTool":false}`
	var got, want any
	if err := json.Unmarshal([]byte(expected), &want); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(output), &got); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("guest replay output = %s, %v; want %s", output, err, expected)
	}
	if sandbox.starts.Load() != 2 || requests.Load() != 2 || retired.Load() != 1 || moved.Load() != 1 {
		t.Fatalf("starts=%d requests=%d list=%d post=%d", sandbox.starts.Load(), requests.Load(), retired.Load(), moved.Load())
	}
}
