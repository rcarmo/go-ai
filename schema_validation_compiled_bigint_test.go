package goai_test

import (
	"encoding/json"
	"math/big"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:470–484. The pinned compiler produced identical exact
// results with these small bound constants represented as JSON numbers.
// The values under test retain their distinct native BigInt domain.
func TestSourceV160SchemaDeclaredBigInt(t *testing.T) {
	const nullable = `{"anyOf":[{"type":"bigint","minimum":0},{"type":"null"}]}`
	compiled := compileV160LeafSchema(t, nullable)
	for _, tc := range []struct {
		name  string
		value any
		want  bool
	}{
		{"L473-positive-bigint", big.NewInt(5), true},
		{"L473-zero-bigint", big.NewInt(0), true},
		{"L473-null", nil, true},
		{"L474-negative-bigint", big.NewInt(-1), false},
		{"L474-number", 5, false},
		{"L474-string", "5", false},
		{"L474-undefined", goai.SchemaUndefined{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertV160BigIntCheck(t, compiled, tc.value, tc.want)
		})
	}
	for _, tc := range []struct {
		name, schema string
		value        *big.Int
		want         bool
	}{
		{"L475-inclusive-maximum-and-multiple", `{"type":"bigint","maximum":10,"multipleOf":5}`, big.NewInt(10), true},
		{"L476-not-a-multiple", `{"type":"bigint","multipleOf":5}`, big.NewInt(7), false},
		{"L477-exclusive-maximum", `{"type":"bigint","exclusiveMaximum":3}`, big.NewInt(3), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertV160BigIntCheck(t, compileV160LeafSchema(t, tc.schema), tc.value, tc.want)
		})
	}
	for _, tc := range []struct {
		name, schema, want string
		value              any
	}{
		{"L478-minimum-diagnostic", `{"type":"bigint","minimum":0}`, `[{"path":"","keyword":"minimum","message":"expected >= 0"}]`, big.NewInt(-2)},
		{"L481-number-type-diagnostic", `{"type":"bigint"}`, `[{"path":"","keyword":"type","message":"expected bigint, got integer"}]`, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issues, err := compileV160LeafSchema(t, tc.schema).Explain(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			assertV160StructuredDiagnostics(t, issues, tc.want)
		})
	}
}

// Supplemental pinned exact boundary values. Reusing the same native value
// pointer must not retain an earlier reference verdict or mutate that value.
func TestSchemaTypedBigIntExactBoundaries(t *testing.T) {
	const schema = `{"$defs":{"integer":{"type":"bigint","minimum":9007199254740992,"exclusiveMaximum":9007199254740994,"multipleOf":2}},"$ref":"#/$defs/integer"}`
	compiled := compileV160LeafSchema(t, schema)
	value := new(big.Int)
	for _, tc := range []struct {
		num    int64
		want   bool
		issues string
	}{
		{9007199254740991, false, `[{"path":"","keyword":"minimum","message":"expected >= 9007199254740992"},{"path":"","keyword":"multipleOf","message":"expected a multiple of 2"}]`},
		{9007199254740992, true, `[]`},
		{9007199254740993, false, `[{"path":"","keyword":"multipleOf","message":"expected a multiple of 2"}]`},
		{9007199254740994, false, `[{"path":"","keyword":"exclusiveMaximum","message":"expected < 9007199254740994"}]`},
	} {
		value.SetInt64(tc.num)
		assertV160BigIntCheck(t, compiled, value, tc.want)
		if got, err := goai.CheckJSONSchemaValueWithOptions(json.RawMessage(schema), value, goai.JSONSchemaCheckOptions{}); err != nil || got != tc.want {
			t.Fatalf("standalone check(%s) = %t, %v; want %t", value, got, err, tc.want)
		}
		issues, err := compiled.Explain(value)
		if err != nil {
			t.Fatal(err)
		}
		assertV160StructuredDiagnostics(t, issues, tc.issues)
		if value.Cmp(big.NewInt(tc.num)) != 0 {
			t.Fatalf("validation mutated the input: got %s; want %d", value, tc.num)
		}
	}
}

func TestSchemaTypedBigIntDoesNotRoundExclusiveBoundary(t *testing.T) {
	const schema = `{"type":"bigint","exclusiveMinimum":9007199254740992}`
	assertV160BigIntCheck(t, compileV160LeafSchema(t, schema), big.NewInt(9007199254740993), true)
}

func TestSchemaTypedBigIntDoesNotChangeJSONOrToolInput(t *testing.T) {
	const schema = `{"type":"bigint"}`
	compiled := compileV160LeafSchema(t, schema)
	assertV160BigIntCheck(t, compiled, json.RawMessage(`9007199254740992`), false)
	assertV160BigIntCheck(t, compiled, json.RawMessage(`"9007199254740992"`), false)
	assertV160BigIntCheck(t, compileV160LeafSchema(t, `{"type":"integer"}`), big.NewInt(2), false)
	tool := goai.Tool{Name: "explicit", Parameters: json.RawMessage(schema)}
	value, err := goai.ValidateToolArguments(&tool, goai.ToolCall{Name: tool.Name, Arguments: big.NewInt(2)})
	if value != nil || err == nil || err.Error() != "tool argument value cannot be cloned: *big.Int" {
		t.Fatalf("tool validation returned %#v, %v; want prior input-domain refusal", value, err)
	}
}

func assertV160BigIntCheck(t *testing.T, compiled *goai.CompiledJSONSchema, value any, want bool) {
	t.Helper()
	got, err := compiled.Check(value)
	if err != nil || got != want {
		t.Errorf("check(%T %v) = %t, %v; want %t", value, value, got, err, want)
	}
}
