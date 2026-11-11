package durable

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"strings"
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

// A fresh canonical task JSON value must not be reinterpreted as the native
// private presence wrapper, even when the value has identical-looking keys.
func TestPiDurableV150TaskWireKeepsWrapperShapedUserData(t *testing.T) {
	raw := []byte(`{"id":2,"conversationId":1,"kind":"compat.task","version":1,"input":{"present":true,"value":"user"},"background":false,"abortRequested":false,"state":{"status":"terminal","outcome":{"status":"completed","result":{"present":true,"value":"result"}}}}`)
	var record TaskRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, raw)) {
		t.Fatalf("wrapper-shaped user data changed: got=%s want=%s", got, raw)
	}
}

// A real private execution envelope must remain readable using both the
// bounded storage codec and standard Snapshot JSON used by crash witnesses.
func TestPiDurableV150PrivateTaskEnvelopeStillRoundTrips(t *testing.T) {
	original := foundationTask(2, JSON{"present": true, "value": "user"})
	original.Status = "done"
	original.Execution.Native.State = TaskState{Status: "terminal", Outcome: &TaskOutcome{Status: "completed", Result: &TaskValue{Present: true, Value: JSON{"present": true, "value": "result"}}}}
	native, err := encodeBounded(original, DefaultLimits(), DefaultLimits().MaxRecordBytes)
	if err != nil {
		t.Fatal(err)
	}
	normal, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range [][]byte{native, normal} {
		var got Task
		if err := decodeStrict(raw, DefaultLimits(), DefaultLimits().MaxRecordBytes, &got); err != nil {
			t.Fatalf("private record decode: %v", err)
		}
		if !equalTaskValue(got.Execution.Native.Input, original.Execution.Native.Input, DefaultLimits()) || !equalTaskValue(got.Execution.Native.State.Outcome.Result, original.Execution.Native.State.Outcome.Result, DefaultLimits()) {
			t.Fatalf("private record was reinterpreted: %s", raw)
		}
	}
}

func TestPiDurableV150PrivateLargeTaskEnvelopeWithinHardLimits(t *testing.T) {
	// The persisted maximum is 1 MiB, higher than the default 512 KiB. Standard
	// crash-witness serialization cannot reject data accepted by that policy.
	limits := DefaultLimits()
	limits.MaxRecordBytes = 1 << 20
	limits.MaxStringBytes = 512 << 10
	original := foundationTask(2, JSON{"a": strings.Repeat("a", 300<<10), "b": strings.Repeat("b", 300<<10)})
	if _, err := copyTask(original, limits); err != nil {
		t.Fatalf("valid larger persisted task rejected: %v", err)
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("valid larger crash witness rejected: %v", err)
	}
	var got Task
	if err := decodeStrict(raw, limits, limits.MaxRecordBytes, &got); err != nil {
		t.Fatalf("larger private record decode: %v", err)
	}
	if !equalTaskValue(got.Execution.Native.Input, original.Execution.Native.Input, limits) {
		t.Fatal("larger private input changed")
	}
}
