package goai_test

import (
	"encoding/json"
	"runtime"
	"strconv"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// packages/schema/test/shared.test.ts:20–24. Go exposes prepared validators,
// not JavaScript check-function identities. This preserves the observable check
// behavior through the public compiler while the private identity assertion is
// recorded as partial in the assertion ledger.
func TestSourceV170SchemaSharedBareChecks(t *testing.T) {
	for _, schema := range []string{
		`{"type":"string","description":"a"}`,
		`{"type":"string","title":"b"}`,
	} {
		compiled, err := goai.CompileJSONSchema(json.RawMessage(schema), goai.JSONSchemaCompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			value string
			want  bool
		}{{`"value"`, true}, {`1`, false}} {
			got, err := compiled.Check(json.RawMessage(tc.value))
			if err != nil || got != tc.want {
				t.Fatalf("check(%s) = %t, %v; want %t", tc.value, got, err, tc.want)
			}
		}
	}
}

// packages/schema/test/shared.test.ts:26–40. The native port has no public
// shared-regex counter or WeakRef. Keep the deterministic production behavior:
// independently prepared patterns work, and releasing unused validators does
// not affect a remaining validator for the same pattern.
func TestSourceV170SchemaSharedPatternChecksSurviveCollection(t *testing.T) {
	validators := make([]*goai.CompiledJSONSchema, 50)
	for i := range validators {
		schema := []byte(`{"type":"string","pattern":"^unique-` + strconv.Itoa(i) + `$"}`)
		compiled, err := goai.CompileJSONSchema(schema, goai.JSONSchemaCompileOptions{})
		if err != nil {
			t.Fatal(err)
		}
		validators[i] = compiled
	}
	again, err := goai.CompileJSONSchema(json.RawMessage(`{"type":"string","pattern":"^unique-0$"}`), goai.JSONSchemaCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	validators = nil
	for range 3 {
		runtime.GC()
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{{`"unique-0"`, true}, {`"unique-1"`, false}} {
		got, err := again.Check(json.RawMessage(tc.value))
		if err != nil || got != tc.want {
			t.Fatalf("check(%s) = %t, %v; want %t", tc.value, got, err, tc.want)
		}
	}
}
