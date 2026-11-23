package durable

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// codemode.test.ts:1102–1151. Run the original script with the original
// catalogue and compare the complete guest object, including empty and
// missing discoveries. This is value equality, not JS host-object identity.
func TestSourceV160CodemodeGuestDiscovery(t *testing.T) {
	const script = `const found = await searchTools("issues");
const byInstructions = await searchTools("repository owner");
const inNamespace = await searchTools("echo", { namespace: "github" });
return {
    found: found.map((tool) => tool.name),
    byInstructions: byInstructions.map((tool) => tool.name),
    inNamespace,
    described: (await describeTool("echo")).includes("echo(args:"),
    missing: (await describeTool("nope")) ?? null,
    namespace: await describeNamespace("github"),
    empty: await describeNamespace("linear"),
    all: ALL_TOOLS.map((tool) => tool.name).includes("list_issues"),
};`
	const expected = `{
        "found":["list_issues"], "byInstructions":["list_issues"], "inNamespace":[],
        "described":true, "missing":null,
        "namespace":{"name":"github", "description":"GitHub MCP server", "instructions":"Pass the repository as owner/name.", "tools":["list_issues"]},
        "empty":{"name":"linear", "description":"Linear: sign in first.", "tools":[]}, "all":true}`
	for _, backend := range []string{"inline", "portable"} {
		t.Run(backend, func(t *testing.T) {
			tool := func(name string) ToolRegistration {
				reg := wrapRegistration(name)
				reg.Definition.Description = name
				reg.Definition.Parameters = json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"id":{"type":"number"}},"additionalProperties":false}`)
				reg.Execute = func(context.Context, any, *ToolAPI) (ToolResult, error) {
					t.Errorf("discovery ran tool %q", name)
					return ToolResult{}, nil
				}
				return reg
			}
			core := &Extension{Name: "coding", Tools: []ToolRegistration{tool("echo"), tool("hold"), tool("pause"), tool("slow")}}
			// Go extension registration names cannot contain colons. The
			// observable namespace names and catalogue stay unchanged.
			github := &Extension{Name: "mcp_github", Namespace: &ToolNamespace{
				Name: "github", Description: "GitHub MCP server", Instructions: "Pass the repository as owner/name.",
			}, Tools: []ToolRegistration{tool("list_issues")}}
			linear := &Extension{Name: "mcp_linear", Namespace: &ToolNamespace{Name: "linear", Description: "Linear: sign in first."}}
			var h *Harness
			var ref ModelRef
			if backend == "inline" {
				h, _, ref = v160NamespaceHarness(t, nil, script, core, github, linear)
			} else {
				guest, _ := newJournalWorkerSandboxV160(t)
				h, _, ref = v160NamespaceHarnessWithSandbox(t, guest, nil, script, core, github, linear)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			conversation := root(t, h, ref)
			sub, err := conversation.Submit(ctx, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Resume(ctx); err != nil {
				t.Fatal(err)
			}
			if result, err := sub.Wait(ctx); err != nil || result.Record.Status != "done" {
				t.Fatalf("submission = %+v, %v", result.Record, err)
			}
			result := v160ToolResult(t, ctx, conversation, "membership")
			_, output, ok := strings.Cut(v160ToolResultText(result), "\nOutput:\n")
			if result.IsError || !ok {
				t.Fatalf("discovery result = %q, error=%t", v160ToolResultText(result), result.IsError)
			}
			var got, want map[string]any
			if err := json.Unmarshal([]byte(expected), &want); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(output), &got); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("discovery object = %s (%v); want %s", output, err, expected)
			}
		})
	}
}

type preDiscoveryWorkerV160 struct{ *journalWorkerRemoteV160 }

func (*preDiscoveryWorkerV160) SupportsWorkerDiscovery() bool { return false }

// Supplemental: an older portable start had no discovery globals. Reopening
// on a host that now advertises them must not change the script's environment.
func TestV160CodemodePortableDiscoveryUsesRecordedStart(t *testing.T) {
	const script = `await tools.hold({}); return [typeof searchTools, typeof describeTool, typeof describeNamespace, typeof models];`
	got := runV160PublishedWorkerNestedReplay(t, script, workerReplayChangesV160{
		beforeStart: func(sandbox *RemoteCodemodeSandbox) {
			sandbox.Remote = &preDiscoveryWorkerV160{sandbox.Remote.(*journalWorkerRemoteV160)}
		},
		beforeResume: func(ctx context.Context, store Storage, sandbox *RemoteCodemodeSandbox) {
			state, err := readStorageProjection(ctx, store, DefaultLimits())
			if err != nil {
				t.Fatal(err)
			}
			found := 0
			for _, doc := range state.Documents {
				if doc.Kind != codemodeRunKind {
					continue
				}
				found++
				if _, present := doc.Value["start"].(map[string]any)["workerDiscovery"]; present {
					t.Fatal("old worker's start unexpectedly recorded discovery")
				}
			}
			if found != 1 {
				t.Fatalf("saved Codemode runs = %d; want 1", found)
			}
			sandbox.Remote = sandbox.Remote.(*preDiscoveryWorkerV160).journalWorkerRemoteV160
		},
	})
	if got.isError || got.text != `["undefined","undefined","undefined","undefined"]` || got.starts != 2 {
		t.Fatalf("legacy environment result = %q, error=%t, guest starts=%d", got.text, got.isError, got.starts)
	}
}

// codemode-describe.test.ts:83. The public system receipt must omit the
// section, rather than merely give the catalogue renderer's empty string.
func TestSourceV160CodemodeEmptyPromptSection(t *testing.T) {
	h, _, ref := v160NamespaceHarness(t, nil, "", &Extension{Name: "coding"})
	system := v160NamespaceSystem(t, root(t, h, ref))
	if section := system.Sections["codemode"]; section != nil {
		t.Fatalf("empty codemode prompt section = %q; want absent", *section)
	}
}
