package durable

import (
	"strings"
	"testing"
)

func TestCodemode120PinnedToolAndGlobalDeclarationBlocks(t *testing.T) {
	tools := []CodemodeToolDescription{{Name: "read", Description: "Read a file.\nSecond line.", InputSchema: JSON{"type": "object", "properties": map[string]any{"path": JSON{"type": "string"}}, "required": []any{"path"}}, OutputSchema: JSON{"type": "string"}}, {Name: "remote-api"}}
	globals := []CodemodeGlobalDeclaration{{Name: "attach", Description: "Attach it.", InputSchema: JSON{"type": "string"}}}
	want := "declare const tools: {\n  /**\n   * Read a file.\n   * Second line.\n   */\n  read(args: { path: string; }): Promise<string>;\n  remote_api(args: unknown): Promise<unknown>;\n};\n\n/** Attach it. */\ndeclare function attach(args: string): Promise<unknown>;"
	if got := RenderCodemodeDeclarations(tools, globals); got != want {
		t.Fatal(got, want)
	}
	globals = []CodemodeGlobalDeclaration{{Name: "models.list", Description: "List models.", Signature: "(type: string): Promise<string[]>"}, {Name: "models.get", InputSchema: JSON{"type": "string"}}, {Name: "plain", Signature: "(): void"}}
	want = "declare function plain(): void;\n\ndeclare const models: {\n  /** List models. */\n  list(type: string): Promise<string[]>;\n  get(args: string): Promise<unknown>;\n};"
	if got := RenderCodemodeDeclarations(nil, globals); got != want {
		t.Fatal(got, want)
	}
	if got := RenderCodemodeDeclarations([]CodemodeToolDescription{{Name: "x", Description: "a */ b"}}, nil); !strings.Contains(got, "/** a *\\/ b */") {
		t.Fatal(got)
	}
}
