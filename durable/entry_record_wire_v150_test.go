package durable

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Records come unchanged from original pi-durable 1.5.0 MemoryStorage
// (scripts/capture-durable-entry-records.mjs). A drop-in Conversation/Entry
// must import and re-export them byte-equivalently.
func TestPiDurableV150ConversationEntryWire(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-durable-v150/entry-records.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Target        string            `json:"target"`
		Conversations []json.RawMessage `json:"conversations"`
		Entries       []json.RawMessage `json:"entries"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil || fixture.Target != "f9a26365568127461c47a07a37dcb42c140a3ac5" {
		t.Fatal("fixture provenance", err)
	}
	check := func(t *testing.T, raw json.RawMessage, target any) {
		if err := json.Unmarshal(raw, target); err != nil {
			t.Fatalf("valid original record rejected: %v", err)
		}
		got, err := json.Marshal(reflect.ValueOf(target).Elem().Interface())
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, raw)) {
			t.Fatalf("got %s\noriginal %s", got, raw)
		}
	}
	for _, raw := range fixture.Conversations {
		t.Run("conversation", func(t *testing.T) { check(t, raw, &Conversation{}) })
	}
	for _, raw := range fixture.Entries {
		t.Run("entry", func(t *testing.T) { check(t, raw, &Entry{}) })
	}
}
