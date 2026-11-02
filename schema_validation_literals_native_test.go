package goai

import (
	"encoding/json"
	"math"
	"math/big"
	"math/rand"
	"reflect"
	"strconv"
	"testing"
)

func TestValidationLiteralNativePrecision(t *testing.T) {
	for _, tc := range []struct {
		a, b interface{}
		want bool
	}{
		{int64(9007199254740993), float64(9007199254740992), false},
		{int64(math.MaxInt64), json.Number("9223372036854775807"), true},
		{int64(math.MaxInt64), float64(math.MaxInt64), false},
		{json.Number("1.0000000000000001"), float64(1), false},
		{json.Number("1000e-3"), int64(1), true},
		{json.Number("-0e2000000000"), float64(0), true},
		{json.Number("1e1000000000"), json.Number("10e999999999"), true},
		{json.Number("1e999999999999999999999"), int64(1), false},
		{true, float64(1), false}, {false, float64(0), false},
		{math.NaN(), float64(1), false}, {math.Inf(1), float64(1), false},
		{map[string]interface{}{"x": true}, map[string]interface{}{"x": float64(1)}, false},
		{[]interface{}{true}, []interface{}{float64(1)}, false},
	} {
		if got := schemaLiteralEqual(tc.a, tc.b); got != tc.want {
			t.Fatalf("%#v vs %#v: %v want %v", tc.a, tc.b, got, tc.want)
		}
		if got := schemaLiteralEqual(tc.b, tc.a); got != tc.want {
			t.Fatal("asymmetric equality")
		}
	}
	// Independent exact rational reference, bounded exponents only. No reference
	// expansion of hostile exponents, and no float-rounded numeric acceptance.
	random := rand.New(rand.NewSource(619))
	for i := 0; i < 2000; i++ {
		n := random.Int63n(2000000000) - 1000000000
		e := random.Intn(41) - 20
		a := json.Number(strconv.FormatInt(n, 10) + "e" + strconv.Itoa(e))
		b := json.Number(strconv.FormatInt(n*10, 10) + "e" + strconv.Itoa(e-1))
		if i%3 == 0 {
			b = json.Number(strconv.FormatInt(n*10+1, 10) + "e" + strconv.Itoa(e-1))
		}
		x, ok := new(big.Rat).SetString(string(a))
		if !ok {
			t.Fatal(a)
		}
		y, ok := new(big.Rat).SetString(string(b))
		if !ok {
			t.Fatal(b)
		}
		if got, want := schemaLiteralEqual(a, b), x.Cmp(y) == 0; got != want {
			t.Fatalf("%s vs %s: %v want %v", a, b, got, want)
		}
	}
}

func TestValidationLiteralContainerGuards(t *testing.T) {
	a := map[string]interface{}{}
	a["self"] = a
	b := map[string]interface{}{}
	b["self"] = b
	if schemaLiteralEqual(a, b) {
		t.Fatal("distinct cyclic non-JSON values must fail closed")
	}
	shared := map[string]interface{}{"n": float64(1)}
	left := []interface{}{shared, shared}
	right := []interface{}{map[string]interface{}{"n": int64(1)}, map[string]interface{}{"n": json.Number("1.0")}}
	if !schemaLiteralEqual(left, right) {
		t.Fatal("noncyclic alias reuse is not inequality")
	}
	if !reflect.DeepEqual(shared, map[string]interface{}{"n": float64(1)}) {
		t.Fatal("input mutated")
	}
}

func TestValidationLiteralReferenceDiagnosticOrder(t *testing.T) {
	raw := []byte(`{"$ref":"#/$defs/C","$defs":{"C":{"const":{"z":"<>&","10":10,"2":"\u2028","a":-0}}}}`)
	var root map[string]interface{}
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	v := newToolSchemaValidator(root, raw)
	target := v.resolve("#/$defs/C").(map[string]interface{})
	if got, want := v.literalJSON(target["const"]), "{\"2\":\"\u2028\",\"10\":10,\"z\":\"<>&\",\"a\":0}"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if v.schemaRaw != nil || len(v.resources.orders) != 0 || len(v.resources.literalOrders) != 1 {
		t.Fatal("retain only required literal data order, not full indexing metadata")
	}
}

func TestValidationLiteralUnchangedEnumWork(t *testing.T) {
	values := make([]interface{}, 10000)
	for i := range values {
		values[i] = "allowed-" + strconv.Itoa(i)
	}
	schema := map[string]interface{}{"enum": values}
	work := &toolSchemaWork{}
	v := newToolSchemaValidator(schema)
	v.work = work
	if got := v.convertValidationValue("not-allowed", schema); got != "not-allowed" {
		t.Fatal(got)
	}
	if work.literalCandidates != 0 {
		t.Fatalf("unchanged candidates rechecked %d times", work.literalCandidates)
	}
	schema = map[string]interface{}{"enum": []interface{}{float64(2), float64(1)}}
	work = &toolSchemaWork{}
	v = newToolSchemaValidator(schema)
	v.work = work
	if got := v.convertValidationValue("1", schema); got != float64(1) || work.literalCandidates != 1 {
		t.Fatalf("got%v checks%d", got, work.literalCandidates)
	}
}
