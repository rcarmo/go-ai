package goai_test

import (
	"encoding/json"
	"math"
	"math/big"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const v160RequiredBigIntCoercion = `{"type":"object","properties":{"value":{"type":"bigint"}},"required":["value"]}`

// coerce.test.ts:142–150. A representable JSON zero is equivalent to the
// original TypeBox minimum 0n on this fixture. Actual results are *big.Int.
func TestSourceV160SchemaPreparedCoerceBigInt(t *testing.T) {
	t.Run("L144-parse-integer-string-with-bound", func(t *testing.T) {
		const schema = `{"type":"object","required":["n"],"properties":{"n":{"type":"bigint","minimum":0}}}`
		compiled := compileV160LeafSchema(t, schema)
		input := map[string]any{"n": "42"}
		value, issues, err := compiled.Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		assertV160PreparedBigIntMember(t, value, "n", big.NewInt(42))
		assertV160StructuredDiagnostics(t, issues, `[]`)
		assertV160PreparedBigIntMember(t, input, "n", "42")
		value, issues, err = compiled.Parse(map[string]any{"n": "-1"})
		if value != nil || err != nil {
			t.Fatalf("out-of-range parse = %#v, %v; want diagnostic rejection", value, err)
		}
		assertV160StructuredDiagnostics(t, issues, `[{"path":"/n","keyword":"minimum","message":"expected >= 0"}]`)
	})

	compiled := compileV160LeafSchema(t, v160RequiredBigIntCoercion)
	for _, tc := range []struct {
		name        string
		input, want any
	}{
		{"L145-bigint-string", "12n", big.NewInt(12)},
		{"L146-decimal-string", "12.9", big.NewInt(12)},
		{"L147-number-truncation", 3.7, big.NewInt(3)},
		{"L148-boolean", true, big.NewInt(1)},
		{"L149-unconvertible-string", "x", "x"},
		{"supplemental-negative-decimal", "-12.9", big.NewInt(-12)},
		{"supplemental-negative-fraction", "-0.8", big.NewInt(0)},
		{"supplemental-large-exact-string", "9007199254740993", big.NewInt(9007199254740993)},
		{"supplemental-uppercase-true", "TRUE", big.NewInt(1)},
		{"supplemental-uppercase-false", "FALSE", big.NewInt(0)},
		{"supplemental-false", false, big.NewInt(0)},
		{"supplemental-null", nil, big.NewInt(0)},
		{"supplemental-undefined-member", goai.SchemaUndefined{}, goai.SchemaUndefined{}},
		{"supplemental-leading-zero", "01", "01"},
		{"supplemental-exponent", "1e3", "1e3"},
		{"supplemental-space", " 12 ", " 12 "},
		{"supplemental-plus-prefix", "+12", "+12"},
		{"supplemental-hex", "0x10", "0x10"},
		{"supplemental-decimal-without-fraction", "12.", "12."},
		{"supplemental-NaN", math.NaN(), math.NaN()},
		{"supplemental-infinity", math.Inf(1), math.Inf(1)},
		{"supplemental-negative-infinity", math.Inf(-1), math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input := map[string]any{"value": tc.input}
			got, err := compiled.Coerce(input)
			if err != nil {
				t.Fatal(err)
			}
			assertV160PreparedBigIntMember(t, got, "value", tc.want)
			assertV160PreparedBigIntMember(t, input, "value", tc.input)
		})
	}
}

// Go integers are mutable, unlike a JavaScript BigInt. Coerce keeps the
// value but does not let a returned pointer rewrite the caller's integer.
func TestSchemaPreparedBigIntCoercionOwnsIntegers(t *testing.T) {
	compiled := compileV160LeafSchema(t, v160RequiredBigIntCoercion)
	original := big.NewInt(9007199254740993)
	input := map[string]any{"value": original}
	result, err := compiled.Coerce(input)
	if err != nil {
		t.Fatal(err)
	}
	assertV160PreparedBigIntMember(t, result, "value", big.NewInt(9007199254740993))
	coerced := result.(map[string]any)["value"].(*big.Int)
	if coerced == original {
		t.Fatal("coercion shares the caller's mutable BigInt pointer")
	}
	coerced.SetInt64(8)
	assertV160PreparedBigIntMember(t, input, "value", big.NewInt(9007199254740993))
	original.SetInt64(7)
	assertV160PreparedBigIntMember(t, result, "value", big.NewInt(8))
}

func TestSchemaPreparedBigIntCoercionPreservesWirePolicy(t *testing.T) {
	compiled := compileV160LeafSchema(t, v160RequiredBigIntCoercion)
	raw := json.RawMessage(`{"value":"12n"}`)
	value, err := compiled.Coerce(raw)
	if err != nil {
		t.Fatal(err)
	}
	assertV160PreparedBigIntMember(t, value, "value", "12n")
	value, issues, err := compiled.Parse(raw)
	if value != nil || err != nil {
		t.Fatalf("parse raw JSON = %#v, %v; want diagnostic rejection", value, err)
	}
	assertV160StructuredDiagnostics(t, issues, `[{"path":"/value","keyword":"type","message":"expected bigint, got string"}]`)

	schema := json.RawMessage(v160RequiredBigIntCoercion)
	input := map[string]any{"value": "12n"}
	value, err = goai.CoerceJSONSchemaValueWithResources(schema, input, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertV160PreparedBigIntMember(t, value, "value", "12n")
	tool := goai.Tool{Name: "raw", Parameters: schema}
	value, err = goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: input})
	if value != nil || err == nil {
		t.Fatalf("tool validation = %#v, %v; want prior input-domain rejection", value, err)
	}
	assertV160DiagnosticLines(t, err, tool.Name, "  - value: expected bigint, got string")
}

func assertV160PreparedBigIntMember(t *testing.T, object any, key string, want any) {
	t.Helper()
	fields, ok := object.(map[string]any)
	if !ok || len(fields) != 1 {
		t.Fatalf("object = %#v; want one %q key", object, key)
	}
	got, present := fields[key]
	if !present {
		t.Fatalf("object = %#v; want present %q key", object, key)
	}
	equal := reflect.DeepEqual(got, want)
	switch expected := want.(type) {
	case *big.Int:
		n, ok := got.(*big.Int)
		equal = ok && n != nil && n.Cmp(expected) == 0
	case float64:
		n, ok := got.(float64)
		equal = ok && math.Float64bits(n) == math.Float64bits(expected)
	}
	if !equal {
		t.Fatalf("value = %T %v; want %T %v", got, got, want, want)
	}
}
