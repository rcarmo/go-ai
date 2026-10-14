package durable

import "testing"

func TestCodemode120PinnedToolSignatureAndSample(t *testing.T) {
	tool := CodemodeToolDescription{Name: "hidden-dynamic-tool", InputSchema: JSON{"type": "object", "properties": map[string]any{"city": JSON{"type": "string"}}, "required": []any{"city"}, "additionalProperties": false}, OutputSchema: JSON{"type": "object", "properties": map[string]any{"ok": JSON{"type": "boolean"}}, "required": []any{"ok"}}}
	if got := RenderCodemodeToolSignature(tool); got != "hidden_dynamic_tool(args: { city: string; }): Promise<{ ok: boolean; }>;" {
		t.Fatal(got)
	}
	if got := RenderCodemodeToolSignature(CodemodeToolDescription{Name: "free"}); got != "free(args: unknown): Promise<unknown>;" {
		t.Fatal(got)
	}
	output := JSON{"type": "object", "properties": map[string]any{"content": JSON{"type": "array", "items": JSON{"type": "object"}}, "isError": JSON{"type": "boolean"}, "_meta": JSON{"type": "object"}}, "required": []any{"content"}}
	want := "plain(args: { [key: string]: unknown; }): Promise<{ _meta?: { [key: string]: unknown; }; content: Array<{ [key: string]: unknown; }>; isError?: boolean; }>;"
	if got := RenderCodemodeToolSignature(CodemodeToolDescription{Name: "plain", InputSchema: JSON{"type": "object", "properties": map[string]any{}}, OutputSchema: output}); got != want {
		t.Fatal(got, want)
	}
	want = "bar\n\ncodemode tool declaration:\n```ts\ndeclare const tools: { foo(args: string): Promise<unknown>; };\n```"
	if got := RenderCodemodeToolSample(CodemodeToolDescription{Name: "foo", Description: "bar", InputSchema: JSON{"type": "string"}}); got != want {
		t.Fatal(got, want)
	}
}
