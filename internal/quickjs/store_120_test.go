package quickjs

import (
	"context"
	"encoding/json"
	"testing"
)

func TestQuickJS120StorePatchStrictAndAtomic(t *testing.T) {
	for _, raw := range []string{`null`, `{}`, `[[]]`, `[[null]]`, `[["a",null]]`, `[["a","bad JSON"]]`, `[["a","1","extra"]]`, `[["a","1"],["a"]]`, `[["valid","1"],[null]]`} {
		patch, err := parseStoreWrites(raw)
		if err == nil || len(patch.Set) != 0 || len(patch.Delete) != 0 {
			t.Fatal("partial patch accepted", raw, patch, err)
		}
	}
	patch, err := parseStoreWrites(`[["delete"],["null","null"],["","{\"n\":1}"]]`)
	if err != nil || len(patch.Delete) != 1 || string(patch.Set["null"]) != "null" || !json.Valid(patch.Set[""]) {
		t.Fatal(patch, err)
	}
}
func TestQuickJS120InvalidCallbackJSONRejectsWithoutPrivateData(t *testing.T) {
	for _, raw := range []json.RawMessage{json.RawMessage(`bad`), json.RawMessage(`{"partial":`), json.RawMessage{}} {
		out, err := boundedQuickJSCallback(context.Background(), func(context.Context, json.RawMessage) (json.RawMessage, error) { return raw, nil }, nil)
		if err == nil || out != nil {
			t.Fatal(out, err)
		}
	}
}
