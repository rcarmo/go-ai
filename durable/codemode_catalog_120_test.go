package durable

import (
	"context"
	"strings"
	"testing"
)

func TestCodemode120IdentifierNamespaceBudgetAndSchema(t *testing.T) {
	for name, want := range map[string]string{"": "_", "123": "_23", "mcp.tool": "mcp_tool", "good_$": "good_$", "😀": "__"} {
		if got := CodemodeIdentifier(name); got != want {
			t.Fatal(name, got, want)
		}
	}
	namespaces := []CodemodeNamespace{{Name: "wide", Prefix: "mcp_"}, {Name: "github", Prefix: "mcp_github_"}, {Name: "files", Prefix: "fs_"}}
	if ns := CodemodeNamespaceOf("mcp_github_issue", namespaces); ns == nil || ns.Name != "github" {
		t.Fatal(ns)
	}
	tools := []CodemodeToolDescription{{Name: "plain", Identifier: "plain", Description: "plain", InputSchema: JSON{"type": "object"}, OutputSchema: JSON{"type": "string"}}, {Name: "mcp_github_issue", Identifier: "mcp_github_issue", Description: "GitHub issue", InputSchema: JSON{"type": "object"}, OutputSchema: JSON{"type": "string"}}, {Name: "fs_read", Identifier: "fs_read", Description: "read files", InputSchema: JSON{"type": "object"}, OutputSchema: JSON{"type": "string"}}}
	section, e := CodemodeSection(tools, namespaces, 0)
	if e != nil || strings.Contains(section, "declare const") || !strings.Contains(section, "tools not listed") {
		t.Fatal(section, e)
	}
	section, e = CodemodeSection(tools, namespaces, 3000)
	if e != nil || !strings.Contains(section, "fs_read") || !strings.Contains(section, "mcp_github_issue") {
		t.Fatal(section, e)
	}
	ranked := SearchCodemodeTools("issue", tools, namespaces, "github", 1)
	if len(ranked) != 1 || ranked[0].ID != "mcp_github_issue" {
		t.Fatal(ranked)
	}
	schema := JSON{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "integer"}, "optional-name": map[string]any{"type": "array", "items": map[string]any{"type": "boolean"}}}, "required": []any{"x"}, "additionalProperties": false}
	rendered := codemodeSchemaType(schema, 0)
	if !strings.Contains(rendered, "x: number;") || !strings.Contains(rendered, `"optional-name"?: Array<boolean>`) {
		t.Fatal(rendered)
	}
}
func TestCodemode120CatalogueCallerIsolationAndCollision(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		work := wrapRegistration("callable")
		work.Deferred = true
		work.Callers = []string{"tools"}
		if e := registry.Register(work); e != nil {
			t.Fatal(e)
		}
		modelOnly := wrapRegistration("model_only")
		modelOnly.Callers = []string{"model"}
		if e := registry.Register(modelOnly); e != nil {
			t.Fatal(e)
		}
		def := taskDefinition(t, "catalog-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			tools, e := r.CodemodeTools(ctx)
			if e != nil {
				return e
			}
			if len(tools) != 1 || tools[0].Name != "callable" || tools[0].OutputSchema["type"] != "string" {
				t.Fatal(tools)
			}
			tools[0].InputSchema["type"] = "changed"
			again, e := r.CodemodeTools(ctx)
			if e != nil || again[0].InputSchema["type"] != "object" {
				t.Fatal("catalog alias", again, e)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record)
		}
	})
}
