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

func TestStreamingMainPinnedReference(t *testing.T) {
	data, err := os.ReadFile("testdata/streaming_main.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []struct {
			Input  string
			Chunks []string
			Frames []struct {
				Present bool
				Value   any
				Changes any
			}
		}
	}
	if err = json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	for index, c := range corpus.Cases {
		t.Run(fmt.Sprint(index), func(t *testing.T) {
			p := &StreamingJSONParser{}
			var history []any
			var edits [][]JSONChange
			for i, chunk := range c.Chunks {
				changes := p.Append(chunk)
				value, present := p.Snapshot()
				want := c.Frames[i]
				if present != want.Present || present && !reflect.DeepEqual(value, want.Value) {
					t.Fatalf("chunk %d %q snapshot=%#v/%v want=%#v/%v", i, chunk, value, present, want.Value, want.Present)
				}
				encoded, err := json.Marshal(changes)
				if err != nil {
					t.Fatal(err)
				}
				var actual any
				if err = json.Unmarshal(encoded, &actual); err != nil {
					t.Fatal(err)
				}
				// Both APIs expose an empty ordered list even when no edit occurred.
				if actual == nil {
					actual = []any{}
				}
				if !reflect.DeepEqual(actual, want.Changes) {
					t.Fatalf("chunk %d edits=%#v want=%#v", i, actual, want.Changes)
				}
				history = append(history, value)
				edits = append(edits, changes)
			}
			for i, value := range history {
				if c.Frames[i].Present && !reflect.DeepEqual(value, c.Frames[i].Value) {
					t.Fatalf("snapshot %d mutated", i)
				}
				encoded, _ := json.Marshal(edits[i])
				var actual any
				json.Unmarshal(encoded, &actual)
				if actual == nil {
					actual = []any{}
				}
				if !reflect.DeepEqual(actual, c.Frames[i].Changes) {
					t.Fatalf("edits %d mutated", i)
				}
			}
		})
	}
}

func TestStreamingMainCreditDepthAndScalarBoundaries(t *testing.T) {
	p := &StreamingJSONParser{}
	if _, present := p.Snapshot(); present {
		t.Fatal("new parser has value")
	}
	p.Append("-0")
	v, present := p.Snapshot()
	if !present || !math.Signbit(v.(float64)) {
		t.Fatal("negative zero lost")
	}
	p = &StreamingJSONParser{}
	p.Append("[" + strings.Repeat("0,", 10000))
	p.Snapshot()
	for i := 0; i < 30; i++ {
		p.Append("1,")
		p.Snapshot()
	}
	old, _ := p.Snapshot()
	p.Append("1")
	lag, _ := p.Snapshot()
	if !reflect.DeepEqual(old, lag) {
		t.Fatal("wide preview ignored copy credit")
	}
	for i := 0; i < 50; i++ {
		p.Append("")
	}
	fresh, ok := p.Snapshot()
	if !ok || len(fresh.([]any)) != 10031 {
		t.Fatal("earned credit did not refresh preview")
	}
	p = &StreamingJSONParser{}
	changes := p.Append(strings.Repeat("[", 40) + "1" + strings.Repeat("]", 40))
	for _, change := range changes {
		if len(change.Path) > 32 {
			t.Fatal("unbounded reported path")
		}
	}
	p = &StreamingJSONParser{}
	p.Append("[" + strings.Repeat("1", 65))
	v, _ = p.Snapshot()
	if len(v.([]any)) != 0 {
		t.Fatal("oversized pending scalar previewed")
	}
}

func FuzzStreamingMainChunkSnapshots(f *testing.F) {
	for _, seed := range []string{`{"a":[1,true,"hi"]}`, `{"a":"bad\descape"}`, `[1e+,null,`, strings.Repeat("[", 40) + "0"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			t.Skip()
		}
		p := &StreamingJSONParser{}
		var snapshots []any
		var encodings [][]byte
		for _, r := range input {
			p.Append(string(r))
			value, _ := p.Snapshot()
			wire, err := json.Marshal(previewTag(value))
			if err != nil {
				t.Fatal(err)
			}
			snapshots = append(snapshots, value)
			encodings = append(encodings, wire)
		}
		for i, value := range snapshots {
			wire, err := json.Marshal(previewTag(value))
			if err != nil {
				t.Fatal(err)
			}
			var before, after any
			json.Unmarshal(encodings[i], &before)
			json.Unmarshal(wire, &after)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("snapshot mutated")
			}
		}
	})
}
