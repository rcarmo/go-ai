package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV170SchemaPreparedIDNHostnameFormatConformance(t *testing.T) {
	checkPinnedSchemaPreparedFormatConformance(t, "v170", preparedFormatFixture{"idn-hostname", 101})
}

// Boundaries captured from the unchanged pinned TypeBox predicate through
// compile.check and compile.errors, not inferred from x/net's lookup profile.
func TestSchemaPreparedIDNHostnameFormatBoundaries(t *testing.T) {
	const schema = `{"type":"string","format":"idn-hostname"}`
	options := goai.JSONSchemaCheckOptions{AssertFormats: true}
	compiled, err := goai.CompileJSONSchema(json.RawMessage(schema), goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: options})
	if err != nil {
		t.Fatal(err)
	}
	domain253 := strings.Join([]string{strings.Repeat("a", 63), strings.Repeat("b", 63), strings.Repeat("c", 63), strings.Repeat("d", 61)}, ".")
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"fullwidth-mapped-label", "ｅｘａｍｐｌｅ．テスト", true},
		{"ignored-soft-hyphen", "example\u00ad.test", true},
		{"ignored-variation-selector", "例\ufe0f.test", true},
		{"normalizes-nfc", "cafe\u0301.test", true},
		{"context-middle-dot", "l\u00b7l", true},
		{"context-middle-dot-invalid", "a\u00b7l", false},
		{"context-greek-valid", "a\u0375\u03b1", true},
		{"disallowed", "a\u302eb", false},
		{"bidi-whole-domain", "0a.א", false},
		{"leading-combining-mark", "\u0301a.test", false},
		{"label-utf16-63", strings.Repeat("a", 63), true},
		{"label-utf16-64", strings.Repeat("a", 64), false},
		{"domain-utf16-253", domain253, true},
		{"domain-utf16-254", domain253 + "e", false},
		{"trailing-stop", "example.", false},
		{"permitted-plus", "a+b", true},
		{"permitted-colon", "a:b", true},
		{"permitted-comma", "a,b", true},
		{"permitted-slash", "a/b", true},
		{"invalid-underscore", "abc_def", false},
		{"invalid-a-label", "xn--7a", false},
		{"punycode-valid", "xn--zca", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := string(encodeV160Schema(t, tc.value))
			assertV160PreparedSchemaCheck(t, compiled, input, tc.valid)
			if got, err := goai.CheckJSONSchemaValueWithOptions(json.RawMessage(schema), json.RawMessage(input), options); err != nil || got != tc.valid {
				t.Fatalf("standalone check(%q) = %t, %v; want %t", tc.value, got, err, tc.valid)
			}
			want := `[]`
			if !tc.valid {
				want = `[{"path":"","keyword":"format","message":"expected format idn-hostname"}]`
			}
			assertV160PreparedMessageRecords(t, compiled, input, want)
		})
	}
}

func TestSchemaIDNHostnameFormatPreservesToolPolicy(t *testing.T) {
	checkSchemaFormatsPreserveToolPolicy(t, "idn-hostname")
}
