package goai_test

import (
	"encoding/json"
	"math"
	"math/big"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestErrorNumberSpellingMain(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"negative-zero", math.Copysign(0, -1), "0"},
		{"float32-negative-zero", float32(math.Copysign(0, -1)), "0"},
		{"decimal-threshold", float64(1e-6), "0.000001"},
		{"scientific-small", float64(1e-7), "1e-7"},
		{"decimal-large", float64(1e20), "100000000000000000000"},
		{"scientific-large", float64(1e21), "1e+21"},
		{"exact-integer", int64(9007199254740993), "9007199254740993"},
		{"exact-uint", uint64(math.MaxUint64), "18446744073709551615"},
		{"exponent-decimal", json.Number("1.230e+06"), "1230000"},
		{"exponent-small", json.Number("1e-06"), "0.000001"},
		{"leading-zero-mantissa", json.Number("0.001e3"), "1"},
		{"negative-scientific", json.Number("-120.0e-8"), "-0.0000012"},
		{"zero-exponent", json.Number("-0e20"), "0"},
		{"native-wide-number", json.Number("12345678901234567890123456789e2"), "1.2345678901234567890123456789e+30"},
		{"string-not-number", "-0 and 1e+06", `"-0 and 1e+06"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := goai.SafeJSONStringify(tc.value); got != tc.want {
				t.Fatal("wrong number text", got, tc.want)
			}
		})
	}
}

func TestErrorNonfiniteMemberNumberSpellingMain(t *testing.T) {
	body := map[string]any{"small": float64(1e-6), "big": float64(1e20), "invalid": math.NaN(), "exact": int64(9007199254740993), "zero": math.Copysign(0, -1)}
	for i := 0; i < 20; i++ {
		text := goai.SafeJSONStringify(body)
		var fields map[string]json.RawMessage
		if err := json.Unmarshal([]byte(text), &fields); err != nil {
			t.Fatal(err)
		}
		for key, want := range map[string]string{"small": "0.000001", "big": "100000000000000000000", "invalid": "null", "exact": "9007199254740993", "zero": "0"} {
			if string(fields[key]) != want {
				t.Fatal("fallback number changed", key, string(fields[key]), want)
			}
		}
	}
}

func TestErrorNumberExactValueMain(t *testing.T) {
	for _, mantissa := range []string{"1", "-123.4500", "0.001", "9007199254740993", "18446744073709551615"} {
		for _, exponent := range []string{"-25", "-7", "-6", "0", "6", "20", "21", "30"} {
			token := mantissa + "e" + exponent
			got := goai.SafeJSONStringify(json.Number(token))
			before, ok := new(big.Rat).SetString(token)
			if !ok {
				t.Fatal("bad reference token", token)
			}
			after, ok := new(big.Rat).SetString(got)
			if !ok || before.Cmp(after) != 0 {
				t.Fatal("precision lost", token, got)
			}
		}
	}
}
