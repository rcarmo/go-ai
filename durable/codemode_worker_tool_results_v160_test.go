package durable

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Source: codemode.test.ts:697–789. These scripts run against real nested
// tools and SQLite with the published portable guest, not canned replies.
func TestSourceV160CodemodePublishedWorkerNestedResultValues(t *testing.T) {
	exitSchema := JSON{"type": "object", "properties": JSON{"output": JSON{"type": "string"}, "exitCode": JSON{"type": "number"}}, "required": []any{"output", "exitCode"}}
	type tool struct {
		name   string
		schema JSON
		run    func(any) ToolResult
	}
	denied := func(any) ToolResult { return ToolResult{Content: "permission denied", IsError: true} }
	for _, tc := range []struct {
		name, script, want string
		tools              []tool
		calls              int32
	}{
		{"schemaless-error", `try { await tools.fails({}); } catch (e) { return [e.message, e.structuredOutput]; }`,
			`["permission denied","permission denied"]`, []tool{{"fails", nil, denied}}, 1},
		{"schema-error", `const r = await tools.run({});
let e; try { await tools.denied({}); } catch (error) { e = [error.message, error.structuredOutput ?? null]; }
return [r.exitCode, r.output, e];`, `[1,"no matches",["permission denied",null]]`, []tool{
			{"run", exitSchema, func(any) ToolResult {
				return ToolResult{Content: "no matches", IsError: true, HasStructuredOutput: true, StructuredOutput: JSON{"output": "no matches", "exitCode": 1}}
			}}, {"denied", exitSchema, denied},
		}, 2},
		{"schemaless-image", `return [await tools.picture({ caption: true }), await tools.picture({ caption: false })];`,
			`["a cat",""]`, []tool{{"picture", nil, func(input any) ToolResult {
				var blocks []goai.ContentBlock
				if input.(JSON)["caption"] == true {
					blocks = append(blocks, goai.ContentBlock{Type: "text", Text: "a cat"})
				}
				const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
				blocks = append(blocks, goai.ContentBlock{Type: "image", MimeType: "image/png", Data: png})
				return ToolResult{Blocks: blocks}
			}}}, 2},
		// Supplemental: a schema tool's own error diagnostic wins over its
		// text, and it still supplies no value for the guest to read.
		{"schema-diagnostic", `try { await tools.denied({}); } catch (e) { return [e.message, e.structuredOutput ?? null]; }`,
			`["reported cause",null]`, []tool{{"denied", exitSchema, func(any) ToolResult {
				return ToolResult{Content: "permission denied", IsError: true, Diagnostics: []ToolDiagnostic{{Severity: "error", Code: "denied", Message: "reported cause"}}}
			}}}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sandbox, remote := newJournalWorkerSandboxV160(t)
			registry := NewRegistry()
			ext, err := NewCodemodeExtension(sandbox)
			if err != nil {
				t.Fatal(err)
			}
			if err := registry.Install(&ext); err != nil {
				t.Fatal(err)
			}
			var calls, requests atomic.Int32
			for _, item := range tc.tools {
				reg := wrapRegistration(item.name)
				reg.StructuredOutputSchema = item.schema
				reg.Execute = func(_ context.Context, args any, _ *ToolAPI) (ToolResult, error) {
					calls.Add(1)
					return item.run(args), nil
				}
				if err := registry.Register(reg); err != nil {
					t.Fatal(err)
				}
			}
			ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
				out := make(chan goai.Event, 1)
				if requests.Add(1) == 1 {
					out <- toolAnswer("s1", "codemode", JSON{"code": tc.script})
				} else {
					out <- terminal("done")
				}
				close(out)
				return out
			})
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			options.Registry = registry
			h := openHarness(t, store, options)
			c := root(t, h, ref)
			sub, err := c.Submit(bg, Input{Content: "go"})
			if err != nil {
				t.Fatal(err)
			}
			if settled := waitSubmission(t, sub).Record; settled.Status != "done" || requests.Load() != 2 {
				t.Fatalf("turn did not complete after one script and final answer: %+v (%d requests)", settled, requests.Load())
			}
			result := v160ToolResult(t, bg, c, "s1")
			_, output, ok := strings.Cut(v160ToolResultText(result), "\nOutput:\n")
			if !ok || result.IsError || output != tc.want {
				t.Errorf("script error=%v output=%q; want %q without error", result.IsError, output, tc.want)
			}
			if calls.Load() != tc.calls || remote.deliveries.Load() != tc.calls || remote.starts.Load() != 1 {
				t.Errorf("tool calls=%d deliveries=%d guest starts=%d; want %d/%d/1", calls.Load(), remote.deliveries.Load(), remote.starts.Load(), tc.calls, tc.calls)
			}
		})
	}
}
