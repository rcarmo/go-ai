package goai_test

import (
	"encoding/json"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV170SchemaPreparedIDNEmailFormatConformance(t *testing.T) {
	checkPinnedSchemaPreparedFormatConformance(t, "v170", preparedFormatFixture{"idn-email", 24})
}

// Supplemental pinned controls distinguish the exact ECMAScript /iu grammar
// from ASCII email checks or Unicode checks that omit case folding and NFC.
func TestSchemaPreparedIDNEmailFormatBoundaries(t *testing.T) {
	const schema = `{"type":"string","format":"idn-email"}`
	compiled, err := goai.CompileJSONSchema(json.RawMessage(schema), goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: goai.JSONSchemaCheckOptions{AssertFormats: true}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"normalized-domain", "user@cafe\u0301.test", true},
		{"folds-greek-ypogegrammeni", "user@\u0345.test", true},
		{"ordinary-combining-mark", "user@\u0301.test", false},
		{"quoted-at", `"a@b"@실례.테스트`, true},
		{"escaped-newline", "\"a\\\nb\"@example.com", false},
		{"unescaped-quoted-newline", "\"a\nb\"@example.com", true},
		{"label-length-63", "user@" + strings.Repeat("a", 63), true},
		{"label-length-64", "user@" + strings.Repeat("a", 64), false},
		{"final-hyphen", "user@a-.example", false},
		{"internal-hyphen", "user@a-b.example", true},
		{"address-literal", "user@[::1]", false},
		{"trailing-newline", "user@example.com\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := string(encodeV160Schema(t, tc.value))
			assertV160PreparedSchemaCheck(t, compiled, input, tc.valid)
			want := `[]`
			if !tc.valid {
				want = `[{"path":"","keyword":"format","message":"expected format idn-email"}]`
			}
			assertV160PreparedMessageRecords(t, compiled, input, want)
		})
	}
}

func TestSchemaIDNEmailOversizedHostInputsFailBeforeQuickJS(t *testing.T) {
	const schema = `{"type":"string","format":"idn-email"}`
	compiled, err := goai.CompileJSONSchema(json.RawMessage(schema), goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: goai.JSONSchemaCheckOptions{AssertFormats: true}})
	if err != nil {
		t.Fatal(err)
	}
	limit := 4 << 20
	for _, tc := range []struct{ name, value string }{
		{"local", strings.Repeat("a", limit+1) + "@example.com"},
		{"domain", "user@" + strings.Repeat("a", limit+1) + ".com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			valid, err := compiled.Check(tc.value)
			if valid || err == nil || !strings.Contains(err.Error(), "schema regexp input exceeds 4194304-byte limit") {
				t.Fatalf("Check oversized %s: valid=%v err=%v", tc.name, valid, err)
			}
			if _, err := compiled.Explain(tc.value); err == nil || !strings.Contains(err.Error(), "schema regexp input exceeds 4194304-byte limit") {
				t.Fatalf("Explain oversized %s: %v", tc.name, err)
			}
		})
	}
}

func TestSchemaIDNEmailFormatPreservesToolPolicy(t *testing.T) {
	checkSchemaFormatsPreserveToolPolicy(t, "idn-email")
}
