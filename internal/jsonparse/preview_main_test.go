package jsonparse

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

// Tagged values preserve UTF-16 units, signed zero and infinities. Ordinary JSON
// fixtures would silently turn lone surrogates into U+FFFD and infinity into null.
func previewTag(value any) any {
	units := func(code []uint16) []any {
		out := make([]any, len(code))
		for i, u := range code {
			out[i] = float64(u)
		}
		return out
	}
	switch v := value.(type) {
	case nil:
		return map[string]any{"null": true}
	case bool:
		return map[string]any{"bool": v}
	case string:
		code, err := StringCodeUnits(v)
		if err != nil {
			panic(err)
		}
		return map[string]any{"string": units(code)}
	case UTF16String:
		return map[string]any{"string": units(v.CodeUnits())}
	case int:
		return map[string]any{"number": float64(v)}
	case float64:
		var n any = v
		if math.IsInf(v, 1) {
			n = "Infinity"
		} else if math.IsInf(v, -1) {
			n = "-Infinity"
		} else if math.IsNaN(v) {
			n = "NaN"
		} else if v == 0 && math.Signbit(v) {
			n = "-0"
		}
		return map[string]any{"number": n}
	case []any:
		a := make([]any, len(v))
		for i, x := range v {
			a[i] = previewTag(x)
		}
		return map[string]any{"array": a}
	case map[string]any:
		out := map[string]any{}
		for key, x := range v {
			code, err := StringCodeUnits(key)
			if err != nil {
				panic(err)
			}
			var token strings.Builder
			for _, u := range code {
				fmt.Fprintf(&token, "%04x", u)
			}
			out[token.String()] = previewTag(x)
		}
		return map[string]any{"object": out}
	default:
		panic(fmt.Sprintf("unknown preview tag type %T", v))
	}
}
func changeTags(changes []JSONChange) []any {
	out := make([]any, len(changes))
	for i, c := range changes {
		path := make([]any, len(c.Path))
		for j, x := range c.Path {
			path[j] = previewTag(x)
		}
		value := map[string]any{"kind": c.Kind, "path": path}
		if c.Kind == "set" {
			value["value"] = previewTag(c.Value)
		} else {
			value["text"] = previewTag(c.Text)
		}
		out[i] = value
	}
	return out
}

func TestStreamingMainLosslessReference(t *testing.T) {
	data, err := os.ReadFile("testdata/streaming_values_main.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Chunks [][]uint16
			Frames []struct {
				Present bool
				Value   any
				Changes []any
			}
		}
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for index, c := range corpus.Cases {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			p := &StreamingJSONParser{}
			var snapshots []any
			var edits [][]JSONChange
			for i, chunk := range c.Chunks {
				changes := p.AppendUTF16(chunk)
				value, present := p.Snapshot()
				want := c.Frames[i]
				if present != want.Present || present && !reflect.DeepEqual(previewTag(value), want.Value) {
					t.Fatalf("chunk %d snapshot=%#v want=%#v", i, previewTag(value), want.Value)
				}
				if actual := changeTags(changes); !reflect.DeepEqual(actual, want.Changes) {
					t.Fatalf("chunk %d changes=%#v want=%#v", i, actual, want.Changes)
				}
				snapshots = append(snapshots, value)
				edits = append(edits, changes)
			}
			for i, value := range snapshots {
				if c.Frames[i].Present && !reflect.DeepEqual(previewTag(value), c.Frames[i].Value) {
					t.Fatalf("snapshot %d mutated", i)
				}
				if !reflect.DeepEqual(changeTags(edits[i]), c.Frames[i].Changes) {
					t.Fatalf("changes %d mutated", i)
				}
			}
		})
	}
}

type previewCaller struct{}

func (previewCaller) MarshalJSON() ([]byte, error) { panic("caller-defined marshaler invoked") }

func TestStreamingMainPreviewWireSafety(t *testing.T) {
	p := &StreamingJSONParser{}
	p.Append(`{"\ud800":"\udfff","n":1e400}`)
	wire, present, err := p.SnapshotJSON()
	if err != nil || !present {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err = json.Unmarshal(wire, &raw); err != nil {
		t.Fatal(err)
	}
	if string(raw["n"]) != "null" {
		t.Fatal("overflow must serialise as JS null")
	}
	// Keep a lossless check on the raw string escapes, not Go's lossy decoder.
	if !strings.Contains(string(wire), `"\ud800":"\udfff"`) {
		t.Fatalf("surrogate units lost on wire: %s", wire)
	}
	if _, err = MarshalParsedJSONValue(map[string]any{"x": previewCaller{}}); err == nil {
		t.Fatal("caller authority accepted")
	}
	if _, err = MarshalParsedJSONValue(map[string]any{string([]byte{0xff}): true}); err == nil {
		t.Fatal("invalid non-WTF-8 key accepted")
	}
	cycle := map[string]any{}
	cycle["self"] = cycle
	if _, err = MarshalParsedJSONValue(cycle); err == nil {
		t.Fatal("cycle accepted")
	}
	// Ordinary Go JSON still rejects nonfinite values: preview serialisation is
	// an explicit, separate adapter, not a replacement for persistence checks.
	if _, err = json.Marshal(math.Inf(1)); err == nil {
		t.Fatal("finite-number check weakened")
	}
}

func FuzzStreamingMainUTF16History(f *testing.F) {
	for _, seed := range [][]byte{{34, 0, 61, 216, 0, 222, 34, 0}, {123, 0, 125, 0}, {34, 0, 0, 216, 88, 0, 34, 0}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1024 {
			t.Skip()
		}
		p := &StreamingJSONParser{}
		var history []any
		var before []any
		for i := 0; i+1 < len(data); i += 2 {
			u := uint16(data[i]) | uint16(data[i+1])<<8
			p.AppendUTF16([]uint16{u})
			v, _ := p.Snapshot()
			history = append(history, v)
			before = append(before, previewTag(v))
		}
		for i, v := range history {
			if !reflect.DeepEqual(previewTag(v), before[i]) {
				t.Fatal("UTF-16 snapshot mutated")
			}
		}
	})
}

func TestStreamingMainPreviewSliceViewsAndLongStrings(t *testing.T) {
	view := make([]any, 1)
	view[0] = view[:0]
	if _, err := marshalPreview(view); err != nil {
		t.Fatal("empty slice view mistaken for cycle", err)
	}
	p := &StreamingJSONParser{}
	p.Append(`"` + strings.Repeat(`\ud83d\ude00`, 20000) + `"`)
	value, present := p.Snapshot()
	if !present || value != strings.Repeat("😀", 20000) {
		t.Fatal("long escaped string differs")
	}
}
