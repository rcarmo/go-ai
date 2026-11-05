package durable

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

// Captured unchanged from original v1.5.0 Session -> MemoryStorage.commit.
func TestPiDurableV150StorageWriteWire(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-durable-v150/storage-writes.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Target  string              `json:"target"`
		Package string              `json:"package"`
		Version string              `json:"version"`
		Types   []string            `json:"types"`
		Batches [][]json.RawMessage `json:"batches"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil || fixture.Target != "f9a26365568127461c47a07a37dcb42c140a3ac5" || fixture.Package != "@earendil-works/pi-durable" || fixture.Version != "1.5.0" || len(fixture.Batches) != 12 || len(fixture.Types) != 8 {
		t.Fatal("fixture provenance", err)
	}
	for batch, writes := range fixture.Batches {
		for index, raw := range writes {
			t.Run(fmt.Sprintf("batch%d/write%d", batch+1, index+1), func(t *testing.T) {
				var write StorageWrite
				if err := json.Unmarshal(raw, &write); err != nil {
					t.Fatalf("valid original command rejected: %v", err)
				}
				got, err := json.Marshal(write)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, raw)) {
					t.Fatalf("got %s\noriginal %s", got, raw)
				}
			})
		}
	}
}

func TestPiDurableV150StorageWriteRejectsMalformedUnion(t *testing.T) {
	for _, raw := range []string{
		`null`, `{}`, `{"type":"unknown"}`,
		`{"type":"conversation","value":null}`,
		`{"type":"conversation","value":{"id":2},"id":2}`,
		`{"type":"entry","value":{"id":2,"conversationId":1,"kind":"note"},"op":"append-entry"}`,
		`{"type":"document.retire","id":2,"content":null}`,
		`{"type":"document.retire","id":0}`,
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"delta","ops":null}}`,
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"delta","ops":[],"value":null}}`,
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"base","value":null}}`,
		`{"type":"document.change","id":2,"content":{"version":0,"kind":"base","value":{}}}`,
		`{"type":"document.copy","record":{"id":3,"kind":"x","scope":{"kind":"session"}},"source":{"id":2}}`,
		`{"type":"document.copy","record":{"id":3,"kind":"x","scope":{"kind":"session"}},"source":{"id":2,"at":null}}`,
		`{"type":"document.create","record":{"id":2,"kind":"x","scope":{"kind":"session","taskId":3}},"content":{"version":1,"kind":"base","value":{}}}`,
		`{"type":"document.create","record":{"id":2,"kind":"x","createdAt":1,"scope":{"kind":"session"}},"content":{"version":1,"kind":"base","value":{}}}`,
		`{"type":"document.create","record":{"id":2,"kind":"x","scope":{"kind":"session"}},"content":{"version":1,"kind":"delta","ops":[]}}`,
		`{"type":"submission","value":{"id":2,"conversationId":1,"type":"continue","status":"queued"}}`,
		`{"type":"submission","value":{"id":2,"conversationId":1,"type":"write","status":"unanswered"}}`,
		`{"type":"submission","value":{"id":2,"conversationId":1,"type":"write","status":"queued","detail":null}}`,
	} {
		t.Run(raw, func(t *testing.T) {
			original := StorageWrite{Type: "document.retire", ID: 99}
			got := original
			if err := json.Unmarshal([]byte(raw), &got); err == nil {
				t.Fatalf("accepted %s", raw)
			}
			if !reflect.DeepEqual(got, original) {
				t.Fatal("failed decode mutated destination")
			}
		})
	}
}

func TestPiDurableV150StorageWritePointsAndEmptyContainers(t *testing.T) {
	for _, raw := range []string{
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"delta","ops":[]}}`,
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"base","value":{}}}`,
		`{"type":"document.create","record":{"id":2,"kind":"x","key":"","scope":{"kind":"session"}},"content":{"version":1,"kind":"base","value":{}}}`,
		`{"type":"document.copy","record":{"id":3,"kind":"x","scope":{"kind":"conversation","conversationId":1},"history":"latest","fork":"current"},"source":{"id":2,"at":"current"}}`,
		`{"type":"document.copy","record":{"id":3,"kind":"x","scope":{"kind":"conversation","conversationId":1},"history":"rewindable","fork":"asOf"},"source":{"id":2,"at":0}}`,
		`{"type":"document.change","id":2,"content":{"version":1,"kind":"base","value":{"n":900719925474099312345,"negative":-0,"exp":1e100}}}`,
	} {
		var write StorageWrite
		if err := json.Unmarshal([]byte(raw), &write); err != nil {
			t.Fatal(err)
		}
		got, err := json.Marshal(write)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, []byte(raw))) {
			t.Fatalf("got %s want %s", got, raw)
		}
	}
}

type storageWireMarshalProbe struct{ called *bool }

func (p storageWireMarshalProbe) MarshalJSON() ([]byte, error) {
	*p.called = true
	return []byte(`{}`), nil
}

func TestPiDurableV150StorageWriteRejectsMarshalAuthorityAndBounds(t *testing.T) {
	called := false
	var typedNil *Entry
	var deep any = nil
	for i := 0; i <= maximumDurableLimits().MaxDepth; i++ {
		deep = []any{deep}
	}
	for _, write := range []StorageWrite{
		{Type: "entry", Value: typedNil},
		{Type: "entry", Value: storageWireMarshalProbe{&called}},
		{Type: "conversation", Value: &Entry{}},
		{Type: "entry", Value: Entry{ID: 2, Conversation: 1, Kind: "x", HasData: true, Data: storageWireMarshalProbe{&called}}},
		{Type: "document.change", ID: 2, Content: &DocumentContent{Version: 1, Kind: "base", Value: JSON{"n": storageWireMarshalProbe{&called}}}},
		{Type: "document.change", ID: 2, Content: &DocumentContent{Version: 1, Kind: "base", Value: JSON{"n": deep}}},
	} {
		if _, err := json.Marshal(write); err == nil {
			t.Fatalf("accepted invalid command %#v", write)
		}
	}
	if called {
		t.Fatal("invoked caller marshal authority")
	}
}

// Public codecs must not rewrite persisted native op/owner/value/point fields.
func TestPiDurableV150StorageWriteKeepsPrivateEnvelope(t *testing.T) {
	const raw = `{"writes":[{"op":"create-conversation","conversation":{"id":2,"parent":1,"parentAt":4,"owner":3}},{"op":"append-entry","entry":{"id":4,"conversation":1,"kind":"message","value":{"role":"user","text":"hi"}}},{"op":"put-submission","submission":{"id":5,"conversation":1,"type":"write","status":"pending","value":{"detail":null}}},{"op":"copy-document","document":{"id":7,"scope":"conversation","owner":2,"kind":"x","key":"","version":0,"value":null},"source":{"id":6,"at":{"seq":1}}}]}`
	var batch Batch
	if err := decodeStrict([]byte(raw), maximumDurableLimits(), maximumDurableLimits().MaxRecordBytes, &batch); err != nil {
		t.Fatal(err)
	}
	got, err := encodeBoundedOrder(batch, maximumDurableLimits(), maximumDurableLimits().MaxRecordBytes, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(dropinJSON(t, got), dropinJSON(t, []byte(raw))) {
		t.Fatalf("private storage envelope changed: %s", got)
	}
}
