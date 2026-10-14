package durable

import "testing"

func TestCodemode120NamespaceAdmissionValidation(t *testing.T) {
	for _, namespaces := range [][]CodemodeNamespace{{{Name: "", Prefix: "a"}}, {{Name: "a", Prefix: ""}}, {{Name: "a-b", Prefix: "a"}, {Name: "a.b", Prefix: "b"}}, {{Name: "a", Prefix: "same"}, {Name: "b", Prefix: "same"}}} {
		if err := validateCodemodeNamespaces(namespaces, DefaultLimits()); err == nil {
			t.Fatal("ambiguous namespaces admitted", namespaces)
		}
	}
	if err := validateCodemodeNamespaces([]CodemodeNamespace{{Name: "parent", Prefix: "mcp."}, {Name: "child", Prefix: "mcp.child."}}, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}
