package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV170SchemaPreparedIRIFormatConformance(t *testing.T) {
	checkPinnedSchemaPreparedFormatConformance(t, "v170", preparedFormatFixture{"iri", 26})
}

func TestSchemaPreparedIRIFormatBoundaries(t *testing.T) {
	const schema = `{"type":"string","format":"iri"}`
	const future = "https://[v1.a]/"
	asciiLength := func(n int) string { return future + strings.Repeat("a", n-len(future)) }
	astralLength := func(n int) string {
		units := n - len(future)
		return future + strings.Repeat("😀", units/2) + strings.Repeat("a", units%2)
	}
	options := goai.JSONSchemaCheckOptions{AssertFormats: true}
	compiled, err := goai.CompileJSONSchema(json.RawMessage(schema), goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, input string
		want        bool
	}{
		{"ipvfuture", future, true},
		{"uppercase-ipvfuture", "https://[Vf.FUTURE]/", true},
		{"bad-ipvfuture-version", "https://[v.z]/", false},
		{"empty-ipvfuture-address", "https://[v1.]/", false},
		{"invalid-angle-bracket", "https://example.com/a<b", false},
		{"invalid-backslash", "https://example.com/a\\b", false},
		{"del-in-path", "https://example.com/a\x7fb", true},
		{"internationalized-host-and-path", "http://ƒøø.ßår/âππ", true},
		{"relative-path", "/âππ", false},
		{"valid-percent-escape", "https://example.com/%AF", true},
		{"bad-percent-escape", "https://example.com/%z", false},
		{"ascii-below-limit", asciiLength(2047), true},
		{"ascii-at-limit", asciiLength(2048), false},
		{"astral-below-unit-limit", astralLength(2047), true},
		{"astral-at-unit-limit", astralLength(2048), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := compiled.Check(tc.input); err != nil || got != tc.want {
				t.Fatalf("prepared check(%s) = %t, %v; want %t", tc.name, got, err, tc.want)
			}
			if got, err := goai.CheckJSONSchemaValueWithOptions(json.RawMessage(schema), tc.input, options); err != nil || got != tc.want {
				t.Fatalf("standalone check(%s) = %t, %v; want %t", tc.name, got, err, tc.want)
			}
			issues, err := compiled.Explain(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want {
				assertV160StructuredDiagnostics(t, issues, `[]`)
			} else {
				assertV160StructuredDiagnostics(t, issues, `[{"path":"","keyword":"format","message":"expected format iri"}]`)
			}
		})
	}
}

func TestSchemaIRIFormatPreservesToolPolicy(t *testing.T) {
	checkSchemaFormatsPreserveToolPolicy(t, "iri")
}
