package goai_test

import (
	"encoding/json"
	"strings"
	"testing"
)

// Source-derived positive/negative controls supplement (not replace) the pinned
// probe table. In particular its multipleOf examples only contain valid values.
func TestV150ValidationConstraintControls(t *testing.T) {
	for _, tc := range []struct{ name, schema, input, want string }{
		{"multiple-valid", `{"properties":{"n":{"type":"number","multipleOf":0.01}}}`, `{"n":0.07}`, `ok {"n":0.07}`},
		{"multiple-invalid", `{"properties":{"n":{"type":"number","multipleOf":0.01}}}`, `{"n":0.075}`, `REJECT - n: expected a multiple of 0.01`},
		{"unique-valid", `{"properties":{"a":{"uniqueItems":true}}}`, `{"a":[1,"1",{"x":1},{"x":2}]}`, `ok {"a":[1,"1",{"x":1},{"x":2}]}`},
		{"maxItems-equal", `{"properties":{"a":{"maxItems":2}}}`, `{"a":[1,2]}`, `ok {"a":[1,2]}`},
		{"minItems-invalid", `{"properties":{"a":{"minItems":2}}}`, `{"a":[1]}`, `REJECT - a: expected at least 2 items`},
		{"maxProperties-invalid", `{"properties":{"a":{"maxProperties":1}}}`, `{"a":{"x":1,"y":2}}`, `REJECT - a: expected at most 1 properties`},
		{"contains-valid", `{"properties":{"a":{"contains":{"type":"number"}}}}`, `{"a":["x",1]}`, `ok {"a":["x",1]}`},
		{"contains-max", `{"properties":{"a":{"contains":{"type":"number"},"minContains":0,"maxContains":1}}}`, `{"a":[1,2]}`, `REJECT - a: expected at most 1 matching item`},
		{"if-else", `{"if":{"required":["x"]},"then":{"required":["y"]},"else":{"required":["z"]}}`, `{}`, `REJECT - z: missing`},
		{"dependent-absent", `{"dependentRequired":{"a":["b"]}}`, `{}`, `ok {}`},
		{"dependent-present", `{"dependentRequired":{"a":["b"]}}`, `{"a":1,"b":2}`, `ok {"a":1,"b":2}`},
		{"dependent-schema", `{"dependentSchemas":{"a":{"required":["b"]}}}`, `{"a":1}`, `REJECT - b: missing`},
		{"not-reject", `{"properties":{"a":{"not":{"type":"number"}}}}`, `{"a":1}`, `REJECT - a: must not match the schema in not`},
		{"unevaluated-allOf", `{"allOf":[{"properties":{"a":{"type":"number"}}},{"properties":{"b":{"type":"string"}}}],"unevaluatedProperties":false}`, `{"a":1,"b":"x"}`, `ok {"a":1,"b":"x"}`},
		{"unevaluated-allOf-extra", `{"allOf":[{"properties":{"a":true}}],"unevaluatedProperties":false}`, `{"a":1,"b":2}`, `REJECT - b: unknown field`},
		{"unevaluated-ref", `{"$defs":{"A":{"properties":{"a":true}}},"$ref":"#/$defs/A","unevaluatedProperties":false}`, `{"a":1}`, `ok {"a":1}`},
		{"tuple-rest-schema", `{"properties":{"a":{"items":[{"type":"number"}],"additionalItems":{"type":"string"}}}}`, `{"a":[1,2]}`, `ok {"a":[1,"2"]}`},
		{"bool-item", `{"properties":{"a":{"items":false}}}`, `{"a":[1]}`, `REJECT - a.0: no value is allowed here`},
		{"unicode-length-valid", `{"properties":{"s":{"minLength":1,"maxLength":1}}}`, `{"s":"😀"}`, `ok {"s":"😀"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := mainNumericOutcome(t, tc.schema, tc.input)
			if strings.HasPrefix(tc.want, "ok ") && strings.HasPrefix(got, "ok ") {
				var value interface{}
				if err := json.Unmarshal([]byte(got[3:]), &value); err != nil {
					t.Fatal(err)
				}
				mainAssertValidationJSON(t, value, tc.want[3:])
				return
			}
			if got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}
