package goai_test

import "testing"

// Source-shaped resolver pointer case: a $ref into data within a nested $id
// inherits that resource's base. Pinned compiler/parse was probed with Bun;
// this is not a verbatim upstream test.
func TestMainValidationReferenceThroughDataUsesResourceBase(t *testing.T) {
	schema := `{"$id":"https://example.test/outer","$defs":{"inner":{"$id":"https://example.test/inner","default":{"$ref":"#/$defs/number"},"$defs":{"number":{"type":"number"}},"properties":{"x":{"$ref":"#/default"}}}},"properties":{"x":{"$ref":"https://example.test/inner"}}}`
	for _, tc := range []struct{ input, want string }{
		{`{"x":{"x":2}}`, `ok {"x":{"x":2}}`},
		{`{"x":{"x":"3"}}`, `ok {"x":{"x":3}}`},
		{`{"x":{"x":"bad"}}`, `REJECT - x.x: expected number, got string`},
		{`{"x":{}}`, `ok {"x":{}}`},
	} {
		t.Run(tc.input, func(t *testing.T) {
			if got := mainNumericOutcome(t, schema, tc.input); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
