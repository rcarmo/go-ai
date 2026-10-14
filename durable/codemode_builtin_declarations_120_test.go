package durable

import (
	"strings"
	"testing"
)

func TestCodemode120SnapshotDeclarationsMatchBuiltinOptIn(t *testing.T) {
	plain := CodemodeStart{Globals: []CodemodeGlobalDescription{{Name: "host.lookup", Version: 1, Signature: "(): Promise<string>"}}}.CodemodeDeclarations()
	for _, name := range []string{"searchTools", "describeTool", "describeNamespace"} {
		if !strings.Contains(plain, "declare function "+name+"(") {
			t.Fatal("missing builtin", name, plain)
		}
	}
	if strings.Contains(plain, "declare const models") {
		t.Fatal("paid metadata exposed")
	}
	if !strings.Contains(plain, "lookup(): Promise<string>") {
		t.Fatal("custom metadata lost", plain)
	}
	paid := CodemodeStart{PaidModels: true}.CodemodeDeclarations()
	for _, name := range []string{"getModelsOfType", "getAvailableOfType", "getModelOfType", "classify", "generateImages"} {
		if !strings.Contains(paid, "  "+name+"(") {
			t.Fatal("missing model builtin", name, paid)
		}
	}
	if strings.Contains(paid, "pi.codemode") {
		t.Fatal("internal tool leaked", paid)
	}
}
