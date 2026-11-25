package goai_test

import (
	"encoding/json"
	"math"
	"math/big"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

const v160RequiredUndefinedCoercion = `{"type":"object","properties":{"value":{"type":"undefined"}},"required":["value"]}`

// coerce.test.ts:152–157. In-memory inputs and outputs preserve undefined
// and BigInt as separate domains, including the required property key.
func TestSourceV160SchemaPreparedCoerceUndefined(t *testing.T) {
	compiled := compileV160LeafSchema(t, v160RequiredUndefinedCoercion)
	for _, tc := range []struct {
		name          string
		input         any
		wantUndefined bool
	}{
		{"L154-null", nil, true},
		{"L154-number-zero", 0, true},
		{"L154-bigint-zero", big.NewInt(0), true},
		{"L154-false", false, true},
		{"L154-empty-string", "", true},
		{"L154-zero-string", "0", true},
		{"L154-null-string", "null", true},
		{"L154-undefined-string", "undefined", true},
		{"L156-wrong-string", "x", false},
		{"supplemental-marker", goai.SchemaUndefined{}, true},
		{"supplemental-uppercase-undefined", "UNDEFINED", true},
		{"supplemental-uppercase-null", "NULL", true},
		{"supplemental-negative-zero", math.Copysign(0, -1), true},
		{"supplemental-spaces", " null ", false},
		{"supplemental-leading-zero-string", "00", false},
		{"supplemental-true", true, false},
		{"supplemental-nonzero-number", 1, false},
		{"supplemental-nonzero-bigint", big.NewInt(1), false},
		{"supplemental-NaN", math.NaN(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := tc.input
			if integer, ok := tc.input.(*big.Int); ok {
				before = new(big.Int).Set(integer)
			}
			input := map[string]any{"value": tc.input}
			want := before
			if tc.wantUndefined {
				want = goai.SchemaUndefined{}
			}
			got, err := compiled.Coerce(input)
			if err != nil {
				t.Fatal(err)
			}
			assertV160UndefinedMember(t, got, want)
			assertV160UndefinedMember(t, input, before)
		})
	}
}

func TestSchemaPreparedUndefinedCoercionPolicy(t *testing.T) {
	compiled := compileV160LeafSchema(t, v160RequiredUndefinedCoercion)
	input := map[string]any{"value": "undefined"}
	t.Run("typed-prepared-input", func(t *testing.T) {
		value, issues, err := compiled.Parse(input)
		if err != nil {
			t.Fatal(err)
		}
		assertV160UndefinedMember(t, value, goai.SchemaUndefined{})
		assertV160StructuredDiagnostics(t, issues, `[]`)
		assertV160UndefinedMember(t, input, "undefined")
	})
	t.Run("raw-json", func(t *testing.T) {
		raw := json.RawMessage(`{"value":"undefined"}`)
		value, err := compiled.Coerce(raw)
		if err != nil {
			t.Fatal(err)
		}
		assertV160UndefinedMember(t, value, "undefined")
		value, issues, err := compiled.Parse(raw)
		if value != nil || err != nil {
			t.Fatalf("parse raw JSON = %#v, %v; want diagnostic rejection", value, err)
		}
		assertV160StructuredDiagnostics(t, issues, `[{"path":"/value","keyword":"type","message":"expected undefined, got string"}]`)
	})
	t.Run("legacy-input", func(t *testing.T) {
		schema := json.RawMessage(v160RequiredUndefinedCoercion)
		value, err := goai.CoerceJSONSchemaValueWithResources(schema, input, nil)
		if err != nil {
			t.Fatal(err)
		}
		assertV160UndefinedMember(t, value, "undefined")
		tool := goai.Tool{Name: "raw", Parameters: schema}
		value, err = goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: input})
		if value != nil || err == nil {
			t.Fatalf("tool validation = %#v, %v; want prior input-domain rejection", value, err)
		}
		assertV160DiagnosticLines(t, err, tool.Name, "  - value: expected undefined, got string")
	})
}

func assertV160UndefinedMember(t *testing.T, object, want any) {
	t.Helper()
	fields, ok := object.(map[string]any)
	if !ok || len(fields) != 1 {
		t.Fatalf("object = %#v; want a single value key", object)
	}
	got, present := fields["value"]
	if !present {
		t.Fatalf("object = %#v; want a present value key", object)
	}
	equal := reflect.DeepEqual(got, want)
	if f, float := want.(float64); float {
		n, ok := got.(float64)
		equal = ok && math.Float64bits(n) == math.Float64bits(f)
	}
	if !equal {
		t.Fatalf("value = %T %#v; want %T %#v", got, got, want, want)
	}
}
