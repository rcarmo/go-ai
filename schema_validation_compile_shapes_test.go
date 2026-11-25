package goai_test

import (
	"bytes"
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:269–271. A non-schema member fails before a value is
// supplied; boolean members are valid schemas with different check results.
func TestSourceV160SchemaCompileMalformedMember(t *testing.T) {
	t.Run("L270-nonschema-property", func(t *testing.T) {
		compiled, err := goai.CompileJSONSchema(json.RawMessage(`{"properties":{"a":1}}`), goai.JSONSchemaCompileOptions{})
		const want = "schema must be an object or a boolean"
		if compiled != nil || err == nil || err.Error() != want {
			t.Fatalf("compile returned validator=%t, error=%v; want %q", compiled != nil, err, want)
		}
	})
	t.Run("supplemental-boolean-members", func(t *testing.T) {
		allow := compileV160LeafSchema(t, `{"properties":{"a":true}}`)
		deny := compileV160LeafSchema(t, `{"properties":{"a":false}}`)
		assertV160PreparedSchemaCheck(t, allow, `{"a":1}`, true)
		assertV160PreparedMessageRecords(t, allow, `{"a":1}`, `[]`)
		assertV160PreparedSchemaCheck(t, deny, `{"a":1}`, false)
		assertV160PreparedMessageRecords(t, deny, `{"a":1}`, `[{"path":"/a","keyword":"false","message":"no value is allowed here"}]`)
	})
}

// validator.test.ts:172–178, using the unchanged TypeBox constructor's exact
// JSON projection without trying to represent its JavaScript metadata.
func TestSourceV160SchemaCompileTypeBoxShape(t *testing.T) {
	const schema = `{"type":"object","required":["name","tags"],"properties":{"name":{"type":"string"},"tags":{"type":"array","items":{"type":"string"}}}}`
	compiled := compileV160LeafSchema(t, schema)
	t.Run("L174-valid-empty-tags", func(t *testing.T) {
		assertV160PreparedSchemaCheck(t, compiled, `{"name":"a","tags":[]}`, true)
	})
	t.Run("L175-invalid-tag-type", func(t *testing.T) {
		assertV160PreparedMessageRecords(t, compiled, `{"name":"a","tags":[1]}`, `[{"path":"/tags/0","keyword":"type","message":"expected string, got integer"}]`)
	})
}

// validator.test.ts:180–187. The native input is caller-owned schema JSON;
// compilation and checks must leave its exact bytes alone.
func TestSourceV160SchemaCompileDoesNotModifyInput(t *testing.T) {
	schema := json.RawMessage(`{"anyOf":[{"type":"object","properties":{"kind":{"const":"text"},"text":{"type":"string"}},"required":["kind","text"],"additionalProperties":false},{"type":"object","properties":{"kind":{"const":"image"},"width":{"type":"integer","minimum":0}},"required":["kind","width"],"additionalProperties":false}],"$defs":{"s":{"type":"string","pattern":"^a"}}}`)
	before := bytes.Clone(schema)
	compiled, err := goai.CompileJSONSchema(schema, goai.JSONSchemaCompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Run("L184-text-branch", func(t *testing.T) {
		assertV160PreparedSchemaCheck(t, compiled, `{"kind":"text","text":"x"}`, true)
	})
	t.Run("L185-one-image-error", func(t *testing.T) {
		assertV160PreparedMessageRecords(t, compiled, `{"kind":"image","width":-1}`, `[{"path":"/width","keyword":"minimum","message":"expected >= 0"}]`)
	})
	t.Run("L186-input-bytes-unchanged", func(t *testing.T) {
		if !bytes.Equal(schema, before) {
			t.Fatalf("schema bytes changed: got %q; want %q", schema, before)
		}
	})
}
