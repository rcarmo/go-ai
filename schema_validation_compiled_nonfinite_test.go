package goai_test

import (
	"encoding/json"
	"math"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:113–133. Both releases use the same schema and native
// comparisons; their pinned compiler and TypeBox runs are separate receipts.
func TestSourceV160SchemaNonFiniteNumberChecks(t *testing.T) { checkPinnedSchemaNonFiniteNumbers(t) }
func TestSourceV161SchemaNonFiniteNumberChecks(t *testing.T) { checkPinnedSchemaNonFiniteNumbers(t) }

func checkPinnedSchemaNonFiniteNumbers(t *testing.T) {
	t.Helper()
	// Use actual NaN and infinity values; JSON would change their domain.
	for _, tc := range []struct {
		name, schema string
		member       bool
		pass, fail   []any
	}{
		{"L115-number", `{"type":"number"}`, false, []any{math.Copysign(0, -1), 1.5, math.MaxFloat64}, nil},
		{"L116-integer", `{"type":"integer"}`, false, []any{2.0}, []any{1.5}},
		{"L117-bounded-number", `{"type":"number","minimum":0}`, false, []any{0.0}, []any{-1.0}},
		{"L118-number-or-string", `{"type":["number","string"]}`, false, []any{1.5, "1.5"}, nil},
		{"L119-number-or-null", `{"anyOf":[{"type":"number"},{"type":"null"}]}`, false, []any{1.5, nil}, nil},
		{"L120-nested-number", `{"type":"object","properties":{"a":{"type":"number"}}}`, true, []any{1.5}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileV160LeafSchema(t, tc.schema)
			wrap := func(value any) any {
				if tc.member {
					return map[string]any{"a": value}
				}
				return value
			}
			for _, nf := range []struct {
				name  string
				value float64
			}{
				{"NaN", math.NaN()}, {"positive-infinity", math.Inf(1)}, {"negative-infinity", math.Inf(-1)},
			} {
				t.Run("L126-L127-"+nf.name, func(t *testing.T) {
					assertV160NonFiniteCheck(t, compiled, wrap(nf.value), false)
				})
			}
			for _, value := range tc.pass {
				assertV160NonFiniteCheck(t, compiled, wrap(value), true)
			}
			for _, value := range tc.fail {
				assertV160NonFiniteCheck(t, compiled, wrap(value), false)
			}
		})
	}
	t.Run("L130-exact-NaN-diagnostic", func(t *testing.T) {
		issues, err := compileV160LeafSchema(t, `{"type":"number"}`).Explain(math.NaN())
		if err != nil {
			t.Fatal(err)
		}
		assertV160StructuredDiagnostics(t, issues, `[{"path":"","keyword":"type","message":"expected number, got NaN"}]`)
	})
}

// Supplemental pinned output: conversion preserves the native number bits;
// the prepared parser still rejects the result and explains the actual value.
func TestSchemaNonFiniteNumberCoercionBoundaries(t *testing.T) {
	const schema = `{"type":"number"}`
	compiled := compileV160LeafSchema(t, schema)
	for _, tc := range []struct {
		name  string
		value float64
	}{
		{"NaN", math.NaN()}, {"Infinity", math.Inf(1)}, {"-Infinity", math.Inf(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			coerced, err := compiled.Coerce(tc.value)
			if n, ok := coerced.(float64); !ok || err != nil || math.Float64bits(n) != math.Float64bits(tc.value) {
				t.Fatalf("coerce(%s) = %T %v, %v; want unchanged typed value", tc.name, coerced, coerced, err)
			}
			value, issues, err := compiled.Parse(tc.value)
			if value != nil || err != nil {
				t.Fatalf("parse(%s) = %v, %v; want diagnostic rejection", tc.name, value, err)
			}
			want := `[{"path":"","keyword":"type","message":"expected number, got ` + tc.name + `"}]`
			assertV160StructuredDiagnostics(t, issues, want)
			if got, err := goai.CheckJSONSchemaValueWithOptions(json.RawMessage(schema), tc.value, goai.JSONSchemaCheckOptions{}); err != nil || got {
				t.Fatalf("standalone check(%s) = %t, %v; want false", tc.name, got, err)
			}
		})
	}
	t.Run("reference-after-coercion", func(t *testing.T) {
		const ref = `{"$defs":{"n":{"type":"number"}},"anyOf":[{"$ref":"#/$defs/n"},{"type":"null"}]}`
		compiled := compileV160LeafSchema(t, ref)
		value, issues, err := compiled.Parse(math.NaN())
		if value != nil || err != nil {
			t.Fatalf("parse referenced NaN = %v, %v; want diagnostic rejection", value, err)
		}
		assertV160StructuredDiagnostics(t, issues, `[{"path":"","keyword":"type","message":"expected number, got NaN"}]`)
	})
}

func assertV160NonFiniteCheck(t *testing.T, compiled *goai.CompiledJSONSchema, value any, want bool) {
	t.Helper()
	got, err := compiled.Check(value)
	if err != nil || got != want {
		t.Fatalf("check(%#v) = %t, %v; want %t", value, got, err, want)
	}
}
