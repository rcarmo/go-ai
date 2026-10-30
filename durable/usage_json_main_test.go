package durable

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type callerUsageMain struct{ goai.Usage }

func (callerUsageMain) MarshalJSON() ([]byte, error) { panic("caller marshaler invoked") }

func TestUsagePresenceMainNativeDTOAndAuthority(t *testing.T) {
	value := struct {
		Usage goai.Usage `json:"usage"`
	}{Usage: goai.Usage{ReasoningSet: true, CacheWrite1hSet: true}}
	encoded, err := encodeBounded(value, DefaultLimits(), DefaultLimits().MaxRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	standard, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var native, reference any
	if err := json.Unmarshal(encoded, &native); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(standard, &reference); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(native, reference) {
		t.Fatalf("native=%#v reference=%#v", native, reference)
	}
	var round struct {
		Usage goai.Usage `json:"usage"`
	}
	if err := decodeStrict(encoded, DefaultLimits(), DefaultLimits().MaxRecordBytes, &round); err != nil {
		t.Fatal(err)
	}
	if !round.Usage.ReasoningSet || !round.Usage.CacheWrite1hSet {
		t.Fatal("native round-trip lost zero presence")
	}
	for _, caller := range []any{callerUsageMain{}, &callerUsageMain{}} {
		if _, err := encodeBounded(caller, DefaultLimits(), 1024); err == nil {
			t.Fatal("caller received native type privilege")
		}
	}
	if _, err := copyObject(JSON{"u": value.Usage}, DefaultLimits()); err == nil {
		t.Fatal("object-only boundary accepted native struct")
	}
}

func TestUsagePresenceMainStrictUnknownFields(t *testing.T) {
	for _, data := range []string{`{"usage":{"reasoning":0,"extra":1}}`, `{"usage":{"cost":{"total":1,"extra":1}}}`} {
		var target struct {
			Usage goai.Usage `json:"usage"`
		}
		if err := decodeStrict([]byte(data), DefaultLimits(), 4096, &target); err == nil {
			t.Fatal("unknown Usage schema field accepted", data)
		}
	}
	var list struct {
		Items []*goai.Usage `json:"items"`
	}
	if err := decodeStrict([]byte(`{"items":[{"reasoning":0,"extra":1}]}`), DefaultLimits(), 4096, &list); err == nil {
		t.Fatal("nested list bypassed strict Usage schema")
	}
	var mapped struct {
		Items map[string]*goai.Usage `json:"items"`
	}
	if err := decodeStrict([]byte(`{"items":{"a":{"reasoning":0,"extra":1}}}`), DefaultLimits(), 4096, &mapped); err == nil {
		t.Fatal("typed map bypassed strict Usage schema")
	}
	dynamic := struct {
		Value any `json:"value"`
	}{Value: &goai.Usage{}}
	if err := decodeStrict([]byte(`{"value":{"reasoning":0,"extra":1}}`), DefaultLimits(), 4096, &dynamic); err == nil {
		t.Fatal("prepopulated interface bypassed strict Usage schema")
	}
}

func TestUsagePresenceMainNativeBudgets(t *testing.T) {
	value := struct {
		Usage goai.Usage `json:"usage"`
	}{Usage: goai.Usage{ReasoningSet: true, CacheWrite1hSet: true}}
	limits := DefaultLimits()
	limits.MaxMembers = 8
	if _, err := encodeBounded(value, limits, 4096); err != nil {
		t.Fatal("private presence metadata changed member budget", err)
	}
	if _, err := encodeBounded(value, limits, 8); err == nil {
		t.Fatal("byte cap weakened")
	}
	limits.MaxNodes = 2
	if _, err := encodeBounded(value, limits, 4096); err == nil {
		t.Fatal("node cap weakened")
	}
	limits = DefaultLimits()
	limits.MaxDepth = 1
	if _, err := encodeBounded(value, limits, 4096); err == nil {
		t.Fatal("depth cap weakened")
	}
}

func TestUsagePresenceMainAliasedTypedPointerStrictness(t *testing.T) {
	// A first embedded allocation and a pointer into it may share an address;
	// traversal deduplication must include type, not hide the Usage's schema.
	type owner struct {
		hidden goai.Usage
		Value  *goai.Usage `json:"value"`
	}
	target := owner{}
	target.Value = &target.hidden
	if err := decodeStrict([]byte(`{"value":{"reasoning":0,"extra":1}}`), DefaultLimits(), 4096, &target); err == nil {
		t.Fatal("same-address typed pointer hid unknown usage field")
	}
}

func FuzzUsagePresenceMainNativeEncoding(f *testing.F) {
	f.Add(int64(0), int64(0), uint8(0))
	f.Add(int64(0), int64(0), uint8(3))
	f.Add(int64(9007199254740993), int64(7), uint8(1))
	f.Fuzz(func(t *testing.T, long, reason int64, flags uint8) {
		value := struct {
			Usage goai.Usage `json:"usage"`
		}{Usage: goai.Usage{CacheWrite1h: int(long), Reasoning: int(reason), CacheWrite1hSet: flags&1 != 0, ReasoningSet: flags&2 != 0}}
		encoded, err := encodeBounded(value, DefaultLimits(), 4096)
		if err != nil {
			t.Fatal(err)
		}
		standard, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		decode := func(data []byte) any {
			var result any
			d := json.NewDecoder(bytes.NewReader(data))
			d.UseNumber()
			if err := d.Decode(&result); err != nil {
				t.Fatal(err)
			}
			return result
		}
		// Compare parsed objects, preserving exact integer tokens (including >2^53),
		// not output bytes, object order or a generated-output digest.
		if !reflect.DeepEqual(decode(encoded), decode(standard)) {
			t.Fatal("native and standard usage objects differ")
		}
		var result struct {
			Usage goai.Usage `json:"usage"`
		}
		if err := decodeStrict(encoded, DefaultLimits(), 4096, &result); err != nil {
			t.Fatal(err)
		}
		if result.Usage.CacheWrite1h != value.Usage.CacheWrite1h || result.Usage.Reasoning != value.Usage.Reasoning || result.Usage.CacheWrite1hSet != (value.Usage.CacheWrite1hSet || value.Usage.CacheWrite1h != 0) || result.Usage.ReasoningSet != (value.Usage.ReasoningSet || value.Usage.Reasoning != 0) {
			t.Fatal("native DTO lost optional counter")
		}
	})
}
