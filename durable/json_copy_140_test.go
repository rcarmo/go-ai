package durable

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Independent original acceptance/normalisation path for differential tests.
func legacyCopyObject140(v JSON, l Limits) (JSON, error) {
	if v == nil {
		return nil, reject("document/checkpoint root must be object")
	}
	b := jsonBudget{l: l, active: map[uintptr]bool{}}
	if err := b.walk(reflect.ValueOf(v), 1, true); err != nil {
		return nil, err
	}
	p, err := encodeBounded(v, l, l.MaxRecordBytes)
	if err != nil {
		return nil, err
	}
	var out JSON
	err = decodeStrict(p, l, l.MaxRecordBytes, &out)
	return out, err
}

func compareCopyObject140(t testing.TB, value JSON, limits Limits) {
	t.Helper()
	want, we := legacyCopyObject140(value, limits)
	got, ge := copyObject(value, limits)
	if (we == nil) != (ge == nil) || we == nil && !reflect.DeepEqual(want, got) {
		t.Fatalf("copy contract mismatch: legacy=%#v/%v native=%#v/%v", want, we, got, ge)
	}
}

func TestCopyObject140DecodedParityLimitsAndDetachment(t *testing.T) {
	type namedMap map[string]any
	type namedString string
	inputs := []JSON{
		nil, {}, {"nil": nil, "map": map[string]any(nil), "array": []any(nil)},
		{"array": []any{}, "map": map[string]any{}},
		{"text": "nul\x00 newline\n quote\" slash\\ Unicode😀", "number": json.Number("9007199254740993"), "negative": json.Number("-0.00e+10")},
		{"fallback": 3, "float": 1.25, "named": namedString("text"), "map": namedMap{"a": true}},
		{"nested": []any{JSON{"enabled": true, "content": strings.Repeat("text", 2000)}}},
		{"invalid": json.Number("01")}, {"invalid": string([]byte{0xff})},
	}
	cycle := JSON{}
	cycle["self"] = cycle
	inputs = append(inputs, cycle)
	for _, input := range inputs {
		for _, kind := range []string{"default", "nodes", "depth", "members", "string", "bytes"} {
			limits := DefaultLimits()
			switch kind {
			case "nodes":
				limits.MaxNodes = 3
			case "depth":
				limits.MaxDepth = 1
			case "members":
				limits.MaxMembers = 1
			case "string":
				limits.MaxStringBytes = 3
			case "bytes":
				limits.MaxRecordBytes = 8
			}
			compareCopyObject140(t, input, limits)
		}
	}
	shared := map[string]any{"a": "original"}
	input := JSON{"first": shared, "second": shared, "array": []any{shared}}
	out, err := copyObject(input, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	out["first"].(map[string]any)["a"] = "changed"
	out["array"].([]any)[0].(map[string]any)["a"] = "array changed"
	if shared["a"] != "original" || out["second"].(map[string]any)["a"] != "original" {
		t.Fatal("container alias leaked")
	}
}

func FuzzCopyObject140DecodedParity(f *testing.F) {
	for _, seed := range []string{`{}`, `{"text":"newline\n\\\"😀","n":9007199254740993}`, `{"nested":[{},null,[],true,1e-6]}`} {
		f.Add([]byte(seed), uint8(0))
	}
	f.Fuzz(func(t *testing.T, data []byte, mode uint8) {
		if len(data) > 64<<10 {
			return
		}
		var input JSON
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if decoder.Decode(&input) != nil {
			return
		}
		limits := DefaultLimits()
		switch mode % 5 {
		case 1:
			limits.MaxNodes = 5
		case 2:
			limits.MaxDepth = 2
		case 3:
			limits.MaxStringBytes = 12
		case 4:
			limits.MaxRecordBytes = 64
		}
		compareCopyObject140(t, input, limits)
	})
}

func BenchmarkCopyObject140Decoded(b *testing.B) {
	value := JSON{"text": strings.Repeat("output line\n", 5000), "nested": []any{JSON{"number": json.Number("9007199254740993"), "flag": true}}}
	for _, candidate := range []struct {
		name string
		copy func(JSON, Limits) (JSON, error)
	}{{"original", legacyCopyObject140}, {"native", copyObject}} {
		b.Run(candidate.name, func(b *testing.B) {
			limits := DefaultLimits()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := candidate.copy(value, limits); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
