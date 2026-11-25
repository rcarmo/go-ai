package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// pi v1.6.1 validator.test.ts:97–111. Go maps have no prototype lookup:
// these names are ordinary keys, and an unrecognized type stays invalid.
func TestSourceV161SchemaPreparedOwnProperties(t *testing.T) {
	const own = `{"type":"object","properties":{"constructor":{"type":"string"},"toString":{"type":"number"}},"required":["constructor"],"additionalProperties":false}`
	compiled, err := goai.CompileJSONSchema(json.RawMessage(own), goai.JSONSchemaCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, value, issues string
		want                bool
	}{
		{"L104-missing-constructor", `{}`, `[{"path":"/constructor","keyword":"required","message":"missing"}]`, false},
		{"L105-own-constructor", `{"constructor":"a"}`, `[]`, true},
		{"L106-toString-wrong-type", `{"constructor":"a","toString":"b"}`, `[{"path":"/toString","keyword":"type","message":"expected number, got string"}]`, false},
		{"supplemental-both-own-and-valid", `{"constructor":"a","toString":1}`, `[]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertV160PreparedSchemaCheck(t, compiled, tc.value, tc.want)
			// The original checks validity; this array is a supplemental
			// exact capture from the pinned explainer.
			assertV160PreparedMessageRecords(t, compiled, tc.value, tc.issues)
		})
	}
	t.Run("L110-unknown-type-is-not-a-predicate", func(t *testing.T) {
		unknown, err := goai.CompileJSONSchema(json.RawMessage(`{"type":"constructor"}`), goai.JSONSchemaCompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		assertV160PreparedSchemaCheck(t, unknown, `1`, false)
		assertV160PreparedMessageRecords(t, unknown, `1`, `[{"path":"","keyword":"type","message":"expected constructor, got integer"}]`)
	})
}
