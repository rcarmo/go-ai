package durable

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

func v160NamespaceGitHub(tools ...ToolRegistration) *Extension {
	return &Extension{Name: "github", Tools: tools, Namespace: &ToolNamespace{
		Name: "mcp__github", Description: "GitHub MCP server", Instructions: "Pass repositories as owner/name.",
	}}
}

func v160NamespaceTool(name, description string) ToolRegistration {
	tool := wrapRegistration(name)
	tool.Definition.Description = description
	tool.Definition.Parameters = json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"}},"required":["path"],"additionalProperties":false}`)
	return tool
}

func v160NamespaceHarness(t *testing.T, budget *int, script string, extensions ...*Extension) (*Harness, *Registry, ModelRef) {
	t.Helper()
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Close(bg) })
	return v160NamespaceHarnessWithSandbox(t, engine, budget, script, extensions...)
}

func v160NamespaceHarnessWithSandbox(t *testing.T, sandbox CodemodeSandbox, budget *int, script string, extensions ...*Extension) (*Harness, *Registry, ModelRef) {
	t.Helper()
	codemode, err := NewCodemodeExtension(sandbox, CodemodeExtensionOptions{InlineBudget: budget})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err = registry.Install(&codemode); err != nil {
		t.Fatal(err)
	}
	for _, extension := range extensions {
		if err = registry.Install(extension); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event, 1)
		if script != "" && requests.Add(1) == 1 {
			ch <- toolAnswer("membership", "codemode", JSON{"code": script})
		} else {
			ch <- terminal("hi")
		}
		close(ch)
		return ch
	})
	options.Registry = registry
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	return openHarness(t, store, options), registry, ref
}

func v160NamespaceSystem(t *testing.T, conversation *ConversationHandle) MessageReceipt {
	t.Helper()
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	sub, err := conversation.Submit(ctx, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := sub.Wait(ctx); err != nil || result.Record.Status != "done" {
		t.Fatalf("submission = %+v, %v", result.Record, err)
	}
	page, err := conversation.Entries(ctx, EntryQuery{}, 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range page.Items {
		if entry.Kind == "pi.system" && len(entry.Model) != 0 && entry.Model[0].Role == goai.RoleSystem {
			return entry.Model[0]
		}
	}
	t.Fatal("missing pi.system message")
	return MessageReceipt{}
}

// codemode-describe.test.ts:70–77 and 126–139. Inspect the actual
// model-facing system entry, not a renderer with synthetic declarations.
func TestSourceV160CodemodeNamespacePromptProjection(t *testing.T) {
	read := v160NamespaceTool("read", "Read a file.")
	issues := v160NamespaceTool("list_issues", "List issues.")
	issues.Callers = []string{"tools"}
	deferred := v160NamespaceTool("get_issue", "Get an issue.")
	deferred.Deferred = true
	h, _, ref := v160NamespaceHarness(t, nil, "", &Extension{Name: "coding", Tools: []ToolRegistration{read}}, v160NamespaceGitHub(issues, deferred))
	assertCodemodeNamespacePrompt(t, v160NamespaceSystem(t, root(t, h, ref)))
}

// Source: pi v1.6.1 codemode-describe.test.ts:109–141. The guest call is
// supplemental: a deferred tool that the prompt omits is still callable.
func TestSourceV161CodemodePublishedNamespacePrompt(t *testing.T) {
	sandbox, remote := newJournalWorkerSandbox(officialWorkerPackageRoot(t, "GOAI_CODEMODE_V161_PACKAGE_ROOT", "1.6.1"))
	read := v160NamespaceTool("read", "Read a file.")
	issues := v160NamespaceTool("list_issues", "List issues.")
	issues.Callers = []string{"tools"}
	deferred := v160NamespaceTool("get_issue", "Get an issue.")
	deferred.Deferred = true
	var calls atomic.Int32
	deferred.Execute = func(_ context.Context, args any, _ *ToolAPI) (ToolResult, error) {
		calls.Add(1)
		if got := args.(JSON)["path"]; got != "fixture" {
			t.Errorf("deferred argument = %v; want fixture", got)
		}
		return ToolResult{Content: "deferred result"}, nil
	}
	const script = `return ["codemode" in tools, await tools.get_issue({path:"fixture"})];`
	h, _, ref := v160NamespaceHarnessWithSandbox(t, sandbox, nil, script,
		&Extension{Name: "coding", Tools: []ToolRegistration{read}}, v160NamespaceGitHub(issues, deferred))
	conversation := root(t, h, ref)
	assertCodemodeNamespacePrompt(t, v160NamespaceSystem(t, conversation))
	result := v160ToolResult(t, bg, conversation, "membership")
	_, body, ok := strings.Cut(v160ToolResultText(result), "Output:\n")
	if result.IsError || !ok || body != `[false,"deferred result"]` {
		t.Fatalf("guest discovery boundary = %q error=%v; want absent self and successful deferred call", v160ToolResultText(result), result.IsError)
	}
	if calls.Load() != 1 || remote.starts.Load() != 1 || remote.deliveries.Load() != 1 {
		t.Fatalf("deferred calls=%d starts=%d replies=%d; want one of each", calls.Load(), remote.starts.Load(), remote.deliveries.Load())
	}
}

func assertCodemodeNamespacePrompt(t *testing.T, system MessageReceipt) {
	t.Helper()
	if system.Sections["codemode"] == nil {
		t.Fatal("missing codemode prompt section")
	}
	section := *system.Sections["codemode"]
	const readHeading, namespaceHeading, issuesHeading = "### `read`", "## mcp__github", "### `list_issues`"
	for _, fragment := range []string{readHeading + "\nRead a file.", "read(args: {", namespaceHeading + "\nGitHub MCP server", issuesHeading, "Promise<string>"} {
		if !strings.Contains(section, fragment) {
			t.Errorf("missing %q in codemode prompt: %s", fragment, section)
		}
	}
	for _, fragment := range []string{"owner/name", "get_issue", "### `codemode`"} {
		if strings.Contains(section, fragment) {
			t.Errorf("unexpected %q in codemode prompt: %s", fragment, section)
		}
	}
	if !(strings.Index(section, readHeading) < strings.Index(section, namespaceHeading) && strings.Index(section, namespaceHeading) < strings.Index(section, issuesHeading)) {
		t.Fatalf("incorrect codemode declaration order: %s", section)
	}
	// The pinned pi-codemode source.ts String.raw value, including its final newline.
	const sourceGrammar = `
start: options_source | plain_source
options_source: OPTIONS_LINE NEWLINE SOURCE
plain_source: SOURCE

OPTIONS_LINE: /[ \t]*\/\/ @options:[^\r\n]*/
NEWLINE: /\r?\n/
SOURCE: /[\s\S]+/
`
	want := &goai.ToolConstrainedSampling{Type: "grammar", Variants: map[string]string{"openai_lark": sourceGrammar}}
	for _, tool := range system.ToolsAdded {
		if tool.Name == "codemode" {
			if !reflect.DeepEqual(tool.ConstrainedSampling, want) {
				t.Fatalf("codemode grammar = %#v, want %#v", tool.ConstrainedSampling, want)
			}
			return
		}
	}
	t.Fatal("missing codemode tool in system entry")
}

// codemode-describe.test.ts:86–95. Two first-generation system entries
// guarantee both renders ran rather than replaying/deduplicating the first.
func TestSourceV160CodemodeNamespaceDeferredPrompt(t *testing.T) {
	coding := &Extension{Name: "coding", Tools: []ToolRegistration{v160NamespaceTool("read", "Read a file.")}}
	h, registry, ref := v160NamespaceHarness(t, nil, "", coding, v160NamespaceGitHub())
	before := v160NamespaceSystem(t, root(t, h, ref)).Sections["codemode"]
	deferred := v160NamespaceTool("list_issues", "List issues.")
	deferred.Deferred = true
	if err := registry.Install(v160NamespaceGitHub(deferred)); err != nil {
		t.Fatal(err)
	}
	afterConversation, err := h.CreateConversation(bg, AgentChange{Model: ref, SystemPrompt: "system"})
	if err != nil {
		t.Fatal(err)
	}
	after := v160NamespaceSystem(t, afterConversation).Sections["codemode"]
	if before == nil || after == nil {
		t.Fatal("missing codemode section before or after deferred arrival")
	}
	if *before != *after {
		t.Fatalf("deferred arrival changed prompt:\nbefore:\n%s\nafter:\n%s", *before, *after)
	}
}

// codemode-describe.test.ts:80–84, 97–107.
func TestSourceV160CodemodeNamespaceBudgetAndEmptyCatalogue(t *testing.T) {
	if section, err := CodemodeSection(nil, nil, 3000); err != nil || section != "" {
		t.Fatalf("empty catalogue = %q, %v", section, err)
	}
	coding := &Extension{Name: "coding", Tools: []ToolRegistration{v160NamespaceTool("read", "Read a file.")}}
	github := make([]ToolRegistration, 30)
	for i := range github {
		github[i] = v160NamespaceTool(fmt.Sprintf("tool%d", i), strings.Repeat("x", 200))
	}
	budget := 400
	h, _, ref := v160NamespaceHarness(t, &budget, "", coding, v160NamespaceGitHub(github...))
	projected := v160NamespaceSystem(t, root(t, h, ref)).Sections["codemode"]
	if projected == nil {
		t.Fatal("missing codemode section")
	}
	for _, fragment := range []string{"### `read`", "Some tools are not listed: find them with `searchTools()`.", "## mcp__github\nGitHub MCP server"} {
		if !strings.Contains(*projected, fragment) {
			t.Errorf("missing %q in budgeted prompt: %s", fragment, *projected)
		}
	}
	for _, fragment := range []string{"tool29", "not listed)"} {
		if strings.Contains(*projected, fragment) {
			t.Errorf("unexpected %q in budgeted prompt: %s", fragment, *projected)
		}
	}
}

// codemode-describe.test.ts:50–60, value-level probe only. The guest sees a
// detached namespace descriptor, not the original JS object identity.
func TestV160CodemodeNamespaceMembershipOrder(t *testing.T) {
	for _, first := range []string{"coding", "github"} {
		t.Run(first, func(t *testing.T) {
			read := v160NamespaceTool("read", "Read a file.")
			coding := &Extension{Name: "coding", Tools: []ToolRegistration{read}}
			github := v160NamespaceGitHub(v160NamespaceTool("list_issues", "List issues."), read)
			extensions := []*Extension{coding, github}
			members := []string{"list_issues"}
			if first == "github" {
				extensions[0], extensions[1] = extensions[1], extensions[0]
				members = append(members, "read")
			}
			const script = `return await describeNamespace("mcp__github");`
			h, _, ref := v160NamespaceHarness(t, nil, script, extensions...)
			conversation := root(t, h, ref)
			v160NamespaceSystem(t, conversation)
			result := v160ToolResult(t, bg, conversation, "membership")
			_, output, ok := strings.Cut(v160ToolResultText(result), "Output:\n")
			var got any
			if !ok || result.IsError {
				t.Fatalf("guest namespace result = %q", v160ToolResultText(result))
			}
			wantMembers := make([]any, len(members))
			for i, member := range members {
				wantMembers[i] = member
			}
			want := map[string]any{"name": "mcp__github", "description": "GitHub MCP server", "instructions": "Pass repositories as owner/name.", "tools": wantMembers}
			if err := json.Unmarshal([]byte(output), &got); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("namespace = %#v, %v; want %#v", got, err, want)
			}
		})
	}
}
