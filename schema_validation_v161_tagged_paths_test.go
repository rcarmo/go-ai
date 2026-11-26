package goai_test

import "testing"

// pi v1.6.1 validator.test.ts:46–57. A selected branch reports its own failure;
// an unknown tag reports the alternatives instead of a generic union mismatch.
func TestSourceV161SchemaPreparedTaggedUnion(t *testing.T) {
	compiled := compileV160LeafSchema(t, v160TaggedDiagnosticsSchema)
	for _, tc := range []struct {
		name, value, issues string
		want                bool
	}{
		{"L48-selected-image-branch", `[{"kind":"text","text":"a"},{"kind":"image","width":-1}]`, `[{"path":"/1/width","keyword":"minimum","message":"expected >= 0"}]`, false},
		{"L54-unrecognized-tag", `[{"kind":"video"}]`, `[{"path":"/0/kind","keyword":"anyOf","message":"expected one of [\"text\",\"image\"]"}]`, false},
		{"supplemental-both-branches-valid", `[{"kind":"text","text":"a"},{"kind":"image","width":0}]`, `[]`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertV160PreparedSchemaCheck(t, compiled, tc.value, tc.want)
			assertV160PreparedMessageRecords(t, compiled, tc.value, tc.issues)
		})
	}
}

// pi v1.6.1 validator.test.ts:59–64. Both reserved characters occur in one
// actual key: slash and tilde must each be escaped once in the error path.
func TestSourceV161SchemaPreparedPointerEscapes(t *testing.T) {
	compiled := compileV160LeafSchema(t, v160PointerDiagnosticsSchema)
	t.Run("L61-escapes-slash-and-tilde", func(t *testing.T) {
		const invalid = `{"a/b~c":1}`
		assertV160PreparedSchemaCheck(t, compiled, invalid, false)
		assertV160PreparedMessageRecords(t, compiled, invalid, `[{"path":"/a~1b~0c","keyword":"type","message":"expected string, got integer"}]`)
	})
	t.Run("supplemental-same-key-valid-value", func(t *testing.T) {
		const valid = `{"a/b~c":"a"}`
		assertV160PreparedSchemaCheck(t, compiled, valid, true)
		assertV160PreparedMessageRecords(t, compiled, valid, `[]`)
	})
}
