package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV161SchemaPreparedIRIReferenceFormatConformance(t *testing.T) {
	checkV161SchemaPreparedFormatConformance(t, preparedFormatFixture{"iri-reference", 21})
}

func TestSchemaPreparedIRIReferenceFormatBoundaries(t *testing.T) {
	const format = "iri-reference"
	schema := json.RawMessage(`{"type":"string","format":"iri-reference"}`)
	options := goai.JSONSchemaCheckOptions{AssertFormats: true}
	compiled, err := goai.CompileJSONSchema(schema, goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, input string
		want        bool
	}{
		{"malformed-scheme", "httpx//example.com", false},
		{"relative-path", "/httpx//example.com", true},
		{"uppercase-malformed-scheme", "HTTPx//example.com", false},
		{"scheme-punctuation", "a+1.2-//example.com", false},
		{"first-digit-is-not-a-scheme", "1http//example.com", true},
		{"invalid-ip-literal", "//[bad]/", false},
		{"percent-escape", "/valid%6f", true},
		{"malformed-percent-escape", "/%6G", false},
		{"raw-space", "/raw space", false},
		{"del-control", "/x\x7f", false},
		{"internationalized-host-and-path", "http://ƒøø.ßår/âππ", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := string(encodeV160Schema(t, tc.input))
			assertV160PreparedSchemaCheck(t, compiled, input, tc.want)
			if got, err := goai.CheckJSONSchemaValueWithOptions(schema, json.RawMessage(input), options); err != nil || got != tc.want {
				t.Fatalf("standalone check(%s) = %t, %v; want %t", input, got, err, tc.want)
			}
			want := `[]`
			if !tc.want {
				want = `[{"path":"","keyword":"format","message":"expected format ` + format + `"}]`
			}
			assertV160PreparedMessageRecords(t, compiled, input, want)
		})
	}
}

func TestSchemaIRIReferenceFormatPreservesToolPolicy(t *testing.T) {
	checkSchemaFormatsPreserveToolPolicy(t, "iri-reference")
}
