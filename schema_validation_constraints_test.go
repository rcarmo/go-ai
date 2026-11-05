package goai

import (
	"encoding/json"
	"math"
	"testing"
)

func TestV150MultipleOfNativePrecision(t *testing.T) {
	for _, tc := range []struct {
		value   interface{}
		divisor float64
		want    bool
	}{
		{json.Number("9007199254740993"), 2, false},
		{int64(9007199254740993), 3, true},
		{json.Number("0.075"), 0.01, false},
		{json.Number("0.07"), 0.01, true},
		{float64(0.0075), 0.0001, true},
		{math.Inf(1), 2, false},
		{json.Number("1e-1000000000"), 0.01, false},
	} {
		if got := schemaNumberMultiple(tc.value, tc.divisor); got != tc.want {
			t.Errorf("%v/%g got %v want %v", tc.value, tc.divisor, got, tc.want)
		}
	}
}

func TestV150UniqueItemsOwnershipAndBounds(t *testing.T) {
	object := map[string]interface{}{"n": json.Number("1.0")}
	if schemaUniqueItems([]interface{}{object, map[string]interface{}{"n": 1}}) {
		t.Fatal("equivalent numeric values accepted as unique")
	}
	if object["n"] != json.Number("1.0") {
		t.Fatal("canonicalization mutated caller")
	}
	if !schemaUniqueItems([]interface{}{object, map[string]interface{}{"n": "1"}}) {
		t.Fatal("number conflated with string")
	}
	cyclic := map[string]interface{}{}
	cyclic["self"] = cyclic
	if schemaUniqueItems([]interface{}{cyclic}) {
		t.Fatal("cyclic native input accepted")
	}
	deep := interface{}("leaf")
	for i := 0; i < 258; i++ {
		deep = []interface{}{deep}
	}
	if schemaUniqueItems([]interface{}{deep}) {
		t.Fatal("unbounded native depth accepted")
	}
	many := make([]interface{}, 10000)
	for i := range many {
		many[i] = i
	}
	if !schemaUniqueItems(many) {
		t.Fatal("large unique array rejected")
	}
}
