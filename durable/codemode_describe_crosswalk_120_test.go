package durable

import (
	"strings"
	"testing"
)

func TestCodemode120PinnedDescribeGroupingAndBudget(t *testing.T) {
	tool := func(name string) CodemodeToolDescription {
		return CodemodeToolDescription{Name: name, Identifier: CodemodeIdentifier(name), InputSchema: JSON{"type": "object", "properties": JSON{"q": JSON{"type": "string"}}}, OutputSchema: JSON{"type": "string"}}
	}
	namespaces := []CodemodeNamespace{{Name: "GitHub", Prefix: "mcp__github__", Description: "Issues and pull requests."}}
	section, err := CodemodeSection([]CodemodeToolDescription{tool("mcp__github__list_issues"), tool("read")}, namespaces, 3000)
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"Tools scripts can call", "### `read`", "## GitHub\nIssues and pull requests.", "list_issues"} {
		if !strings.Contains(section, part) {
			t.Fatal(part, section)
		}
	}
	if strings.Index(section, "### `read`") > strings.Index(section, "## GitHub") {
		t.Fatal("standalone group order", section)
	}
	huge := tool("mcp__github__huge")
	huge.Description = strings.Repeat("long ", 1000)
	section, err = CodemodeSection([]CodemodeToolDescription{huge, tool("mcp__github__tiny"), tool("read")}, namespaces, 100)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(section, "## GitHub (some tools not listed)") || !strings.Contains(section, "### `mcp__github__tiny`") || strings.Contains(section, "### `mcp__github__huge`") {
		t.Fatal("budget", section)
	}
	section, err = CodemodeSection([]CodemodeToolDescription{tool("mcp__github__tiny")}, namespaces, 0)
	if err != nil || !strings.Contains(section, "## GitHub (tools not listed)") {
		t.Fatal(section, err)
	}
	section, err = CodemodeSection(nil, namespaces, 3000)
	if err != nil || section != "" {
		t.Fatal(section, err)
	}
	found := CodemodeNamespaceOf("mcp__github__list", []CodemodeNamespace{{Name: "MCP", Prefix: "mcp__"}, namespaces[0]})
	if found == nil || found.Name != "GitHub" {
		t.Fatal(found)
	}
}
