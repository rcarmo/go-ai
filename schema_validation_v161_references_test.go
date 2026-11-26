package goai_test

import (
	"encoding/json"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// pi v1.6.1 validator.test.ts:149–157. Both checks use the same prepared
// validator and the caller's offline document registry.
func TestSourceV161SchemaPreparedRegisteredReference(t *testing.T) {
	const uri = "https://example.com/point.json"
	const schema = `{"$ref":"https://example.com/point.json"}`
	point := json.RawMessage(`{"type":"object","properties":{"x":{"type":"number"}},"required":["x"]}`)
	options := goai.JSONSchemaCompileOptions{JSONSchemaCheckOptions: goai.JSONSchemaCheckOptions{Resources: map[string]json.RawMessage{uri: point}}}
	compiled, err := goai.CompileJSONSchema(json.RawMessage(schema), options)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("L155-registered-point-passes", func(t *testing.T) {
		assertV160PreparedSchemaCheck(t, compiled, `{"x":1}`, true)
		assertV160PreparedMessageRecords(t, compiled, `{"x":1}`, `[]`)
	})
	t.Run("L156-missing-point-coordinate-fails", func(t *testing.T) {
		assertV160PreparedSchemaCheck(t, compiled, `{}`, false)
		assertV160PreparedMessageRecords(t, compiled, `{}`, `[{"path":"/x","keyword":"required","message":"missing"}]`)
	})
	t.Run("supplemental-coordinate-is-not-coerced", func(t *testing.T) {
		assertV160PreparedSchemaCheck(t, compiled, `{"x":"1"}`, false)
		assertV160PreparedMessageRecords(t, compiled, `{"x":"1"}`, `[{"path":"/x","keyword":"type","message":"expected number, got string"}]`)
	})
}
