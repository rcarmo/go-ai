package durable

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestImmutableValueRoundTripAndDetachedCopy(t *testing.T) {
	value, err := NewImmutableValue(JSON{
		"number": json.Number("42"),
		"text":   "hello",
		"items":  []any{true, JSON{"nested": 1}},
		"flag":   false,
	}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if !value.IsObject() {
		t.Fatal("root value should be an object")
	}
	if got, ok := value.Get("number"); !ok || got.Number() != json.Number("42") {
		t.Fatal("number was not preserved exactly")
	}
	if got, ok := value.Get("text"); !ok || got.String() != "hello" {
		t.Fatal("string was not preserved")
	}
	items, ok := value.Get("items")
	if !ok || !items.IsArray() {
		t.Fatal("array accessor failed")
	}
	if got, ok := items.Index(1); !ok {
		t.Fatal("nested array element missing")
	} else if nested, ok := got.Get("nested"); !ok || nested.Number() != json.Number("1") {
		t.Fatal("nested object lookup failed")
	}
	copyValue := value.Copy()
	mut := copyValue.(map[string]any)
	mut["text"] = "changed"
	if got, ok := value.Get("text"); !ok || got.String() != "hello" {
		t.Fatal("mutating a detached copy leaked into the immutable value")
	}
}

func TestImmutableValuePreservesObjectIdentityForUnchangedNodes(t *testing.T) {
	base := JSON{
		"docs": JSON{
			"pi.live":  JSON{"text": "hello"},
			"pi.agent": JSON{"thinkingLevel": "high"},
		},
	}
	value, err := NewImmutableValue(base, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	docs, ok := value.Get("docs")
	if !ok {
		t.Fatal("docs missing")
	}
	object, ok := docs.Object()
	if !ok {
		t.Fatal("object view missing")
	}
	left, ok := object.Get("pi.live")
	if !ok {
		t.Fatal("pi.live missing")
	}
	right, ok := object.Get("pi.live")
	if !ok {
		t.Fatal("pi.live missing")
	}
	if left != right {
		t.Fatal("unchanged node handles should stay identical")
	}
	if got, ok := left.Get("text"); !ok || got.String() != "hello" {
		t.Fatal("scalar lookup on a shared node failed")
	}
}

func TestImmutableValueRejectsUnsafeInputs(t *testing.T) {
	if _, err := NewImmutableValue(JSON{"bad": func() {}}, DefaultLimits()); err == nil {
		t.Fatal("function values must be rejected")
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	if _, err := NewImmutableValue(JSON{"cycle": cycle}, DefaultLimits()); err == nil {
		t.Fatal("cyclic map values must be rejected")
	}
	if _, err := NewImmutableValue(JSON{"number": mathNaN()}, DefaultLimits()); err == nil {
		t.Fatal("nonfinite numbers must be rejected")
	}
	if value, err := NewImmutableValue(nil, DefaultLimits()); err != nil || value.Kind() != "null" {
		t.Fatal("raw JSON null should be accepted")
	}
}

func mathNaN() float64 { return math.NaN() }

func TestImmutableDocumentCopyAndKey(t *testing.T) {
	doc, err := NewImmutableDocument("pi.live", JSON{"text": "x"}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if doc.Key() != "pi.live" {
		t.Fatal("document key mismatch")
	}
	copyValue, ok := doc.Value().Object()
	if !ok {
		t.Fatal("document value is not an object")
	}
	if got, ok := copyValue.Get("text"); !ok || got.String() != "x" {
		t.Fatal("document value mutation guard failed")
	}
	mut := doc.Copy().(map[string]any)
	mut["text"] = "changed"
	if got, ok := doc.Value().Get("text"); !ok || got.String() != "x" {
		t.Fatal("document copy mutation leaked")
	}
}

func TestImmutableMapAndArrayAccessors(t *testing.T) {
	value, err := NewImmutableValue(JSON{"items": []any{1, 2, 3}, "meta": JSON{"ok": true}}, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	items, ok := value.Get("items")
	if !ok || !items.IsArray() {
		t.Fatal("items is not an array")
	}
	if got, ok := items.Index(0); !ok || got.Number() != json.Number("1") {
		t.Fatal("array elements should preserve numeric values")
	}
	meta, ok := value.Get("meta")
	if !ok || !meta.IsObject() {
		t.Fatal("meta is not an object")
	}
	if got, ok := meta.Get("ok"); !ok || !got.Bool() {
		t.Fatal("meta.ok should be true")
	}
	if reflect.DeepEqual(value.Copy(), value.Copy()) == false {
		t.Fatal("copy should be stable")
	}
}

func TestImmutableValueValidatesPolicyAndPreservesExactNumbers(t *testing.T) {
	limits := DefaultLimits()
	limits.MaxNodes = 1 << 30
	if _, err := NewImmutableValue(JSON{}, limits); err == nil {
		t.Fatal("invalid resource policy accepted")
	}
	original := JSON{"wide": int64(9223372036854775807), "decimal": json.Number("9007199254740993"), "nested": JSON{"text": "original"}}
	value, err := NewImmutableValue(original, DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	original["nested"].(JSON)["text"] = "changed"
	wide, _ := value.Get("wide")
	decimal, _ := value.Get("decimal")
	nested, _ := value.Get("nested")
	text, _ := nested.Get("text")
	if wide.Number() != json.Number("9223372036854775807") || decimal.Number() != json.Number("9007199254740993") || text.String() != "original" {
		t.Fatal("input alias or numeric rounding")
	}
	keys := value.Keys()
	keys[0] = "changed"
	if _, found := value.Get("changed"); found {
		t.Fatal("key slice exposed mutable authority")
	}
}

func TestImmutableDocumentRequiresBoundedObject(t *testing.T) {
	if _, err := NewImmutableDocument("pi.live", nil, DefaultLimits()); err == nil {
		t.Fatal("null document accepted")
	}
	limits := DefaultLimits()
	limits.MaxDocumentBytes = 64
	if _, err := NewImmutableDocument("pi.live", JSON{"text": strings.Repeat("x", 65)}, limits); err == nil {
		t.Fatal("document byte limit bypassed")
	}
	if _, err := NewImmutableDocument("", JSON{}, DefaultLimits()); err == nil {
		t.Fatal("invalid document kind accepted")
	}
}
