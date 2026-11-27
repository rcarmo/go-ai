package goai_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"unicode/utf16"

	goai "github.com/rcarmo/go-ai"
)

// validator.test.ts:373–380. The case is byte-identical across the two
// releases; the pinned runs and code-unit captures are separate receipts.
func TestSourceV160SchemaCompiledUTF16Length(t *testing.T) { checkPinnedSchemaUTF16Length(t) }
func TestSourceV161SchemaCompiledUTF16Length(t *testing.T) { checkPinnedSchemaUTF16Length(t) }

func checkPinnedSchemaUTF16Length(t *testing.T) {
	t.Helper()
	// Verify the exact UTF-16 units before checking length: a replacement
	// rune could produce the same verdict despite losing an original unit.
	const minTwo = `{"type":"string","minLength":2}`
	const maxOne = `{"type":"string","maxLength":1}`
	for _, tc := range []struct {
		name, schema, raw string
		units             []uint16
		isolated, want    bool
	}{
		{"L374-unpaired-plus-letter", minTwo, `"\ud800A"`, []uint16{0xd800, 'A'}, true, true},
		{"L375-unpaired-plus-letter-too-long", maxOne, `"\ud800A"`, []uint16{0xd800, 'A'}, true, false},
		{"L376-unpaired-high", maxOne, `"\ud800"`, []uint16{0xd800}, true, true},
		{"L377-emoji-pair", maxOne, `"😀"`, []uint16{0xd83d, 0xde00}, false, true},
		{"L378-emoji-pair-too-short", minTwo, `"😀"`, []uint16{0xd83d, 0xde00}, false, false},
		{"L379-unpaired-low-then-high", minTwo, `"\udc00\ud800"`, []uint16{0xdc00, 0xd800}, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertV160UTF16Length(t, tc.schema, tc.raw, tc.units, tc.isolated, tc.want)
		})
	}
}

// Supplemental pinned predicate result on two code points and three units
// within one exceptional string, plus a valid pair expressed by JSON escapes.
func TestSchemaCompiledUTF16MixedPair(t *testing.T) {
	assertV160UTF16Length(t, `{"type":"string","minLength":2,"maxLength":2}`, `"\ud800\ud83d\ude00"`,
		[]uint16{0xd800, 0xd83d, 0xde00}, true, true)
	assertV160UTF16Length(t, `{"type":"string","maxLength":1}`, `"\ud83d\ude00"`,
		[]uint16{0xd83d, 0xde00}, false, true)
}

func assertV160UTF16Length(t *testing.T, schema, raw string, units []uint16, isolated, want bool) {
	t.Helper()
	var call goai.ToolCall
	if err := json.Unmarshal([]byte(`{"name":"unicode","arguments":`+raw+`}`), &call); err != nil {
		t.Fatal(err)
	}
	var gotUnits []uint16
	if isolated {
		text, ok := call.Arguments.(goai.UTF16String)
		if !ok {
			t.Fatalf("decoded %s as %#v (%T); want lossless UTF16String", raw, call.Arguments, call.Arguments)
		}
		gotUnits = text.CodeUnits()
	} else {
		text, ok := call.Arguments.(string)
		if !ok {
			t.Fatalf("decoded %s as %T; want ordinary Unicode string", raw, call.Arguments)
		}
		gotUnits = utf16.Encode([]rune(text))
	}
	if !reflect.DeepEqual(gotUnits, units) {
		t.Fatalf("code units for %s = %x; want %x", raw, gotUnits, units)
	}
	compiled := compileV160LeafSchema(t, schema)
	got, err := compiled.Check(call.Arguments)
	if err != nil || got != want {
		t.Errorf("compiled check(%s) = %t, %v; want %t", raw, got, err, want)
	}
	got, err = goai.CheckJSONSchemaValueWithOptions(json.RawMessage(schema), call.Arguments, goai.JSONSchemaCheckOptions{})
	if err != nil || got != want {
		t.Errorf("standalone check(%s) = %t, %v; want %t", raw, got, err, want)
	}
}
