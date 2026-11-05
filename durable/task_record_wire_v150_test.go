package durable

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Expected values come directly from original pi-durable1.5 MemoryStorage,
// with no Go-side field aliases, wrapper removal or presence normalization.
func dropinTaskFixtures(t *testing.T) []struct {
	Name   string
	Record json.RawMessage
} {
	t.Helper()
	data, err := os.ReadFile("testdata/pi-durable-v150/task-records.json")
	if err != nil {
		t.Fatal(err)
	}
	var source struct {
		Target   string
		Package  string
		Version  string
		Fixtures []struct {
			Name   string
			Record json.RawMessage
		}
	}
	if err := json.Unmarshal(data, &source); err != nil {
		t.Fatal(err)
	}
	if source.Target != "f9a26365568127461c47a07a37dcb42c140a3ac5" || source.Package != "@earendil-works/pi-durable" || source.Version != "1.5.0" || len(source.Fixtures) != 13 {
		t.Fatal("wrong original fixture provenance")
	}
	return source.Fixtures
}
func dropinJSON(t *testing.T, data []byte) any {
	t.Helper()
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var result any
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestPiDurableV150TaskRecordWireRoundTrip(t *testing.T) {
	for _, fixture := range dropinTaskFixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			var record TaskRecord
			if err := json.Unmarshal(fixture.Record, &record); err != nil {
				t.Fatalf("valid original record rejected: %v", err)
			}
			got, err := json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, fixture.Record)) {
				t.Fatalf("original record changed\ngot  %s\nwant %s", got, fixture.Record)
			}
		})
	}
}
func TestPiDurableV150TaskStateWireRoundTrip(t *testing.T) {
	for _, fixture := range dropinTaskFixtures(t) {
		t.Run(fixture.Name, func(t *testing.T) {
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(fixture.Record, &raw); err != nil {
				t.Fatal(err)
			}
			var state TaskState
			if err := json.Unmarshal(raw["state"], &state); err != nil {
				t.Fatalf("valid original state rejected: %v", err)
			}
			got, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, raw["state"])) {
				t.Fatalf("original state changed\ngot  %s\nwant %s", got, raw["state"])
			}
		})
	}
}
