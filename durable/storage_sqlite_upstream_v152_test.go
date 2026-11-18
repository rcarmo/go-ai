package durable

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSourceV152SQLiteViewerReadsUpstreamProducedDatabase(t *testing.T) {
	bytesBefore, err := os.ReadFile("testdata/pi-durable-v152/viewer.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "session.sqlite")
	if err := os.WriteFile(path, bytesBefore, 0600); err != nil {
		t.Fatal(err)
	}
	store, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatalf("read upstream-produced SQLite: %v", err)
	}
	defer store.Close(bg)
	conv, found, err := store.Conversation(bg, 1)
	if err != nil || !found || conv.ID != 1 {
		t.Fatal(conv, found, err)
	}
	entries, err := store.ScanEntries(bg, EntryQuery{Conversation: 1}, 10, nil)
	if err != nil || len(entries.Items) != 2 || entries.Items[0].ID != 7 || entries.Items[1].ID != 3 {
		t.Fatal(entries, err)
	}
	entry, found, err := store.Entry(bg, 1, 3)
	if err != nil || !found || entry.CommitSeq != 1 || entry.Entry.Data != "first" {
		t.Fatal(entry, found, err)
	}
	head, found, err := store.FindLatestHeadMarker(bg, 1, 7)
	if err != nil || !found || head.ID != 7 || head.Head != 7 || !equalJSONValue(head.Data, JSON{"done": true}) {
		t.Fatal(head, found, err)
	}
	task, found, err := store.Task(bg, 4)
	if err != nil || !found {
		t.Fatal(task, found, err)
	}
	if got, err := json.Marshal(task); err != nil || !jsonValuesEqual(got, []byte(`{"id":4,"conversationId":1,"kind":"test.task","version":1,"input":{"x":2},"background":false,"abortRequested":false,"state":{"status":"running","checkpoint":{"phase":"run"}}}`)) {
		t.Fatal(string(got), err)
	}
	submission, found, err := store.SubmissionByRequest(bg, 1, "request")
	if err != nil || !found || submission.ID != 5 || submission.Status != "placed" || submission.Entry != 3 {
		t.Fatal(submission, found, err)
	}
	for seq, text := range map[uint64]string{1: "before", 2: "after"} {
		point := DocumentPoint{Seq: seq}
		addr := DocumentAddress{Scope: "conversation", Owner: 1, Kind: "test.document"}
		record, found, err := store.FindDocument(bg, addr, point)
		if err != nil || !found || record.ID != 6 || record.CreatedAt != 1 {
			t.Fatal(record, found, err)
		}
		content, found, err := store.DocumentAt(bg, record.ID, point)
		if err != nil || !found || content.Version != 1 || !equalJSONValue(content.Value, JSON{"text": text}) {
			t.Fatal(content, found, err)
		}
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	if writer, err := OpenSQLite(path, SQLiteOptions{}); !errors.Is(err, ErrReadOnlyStorage) {
		if writer != nil {
			_ = writer.Close(bg)
		}
		t.Fatalf("writable open must refuse before modifying upstream schema: %v", err)
	}
	bytesAfter, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(bytesBefore, bytesAfter) {
		t.Fatal("upstream database changed", err)
	}
}

func jsonValuesEqual(a, b []byte) bool {
	var x, y any
	return json.Unmarshal(a, &x) == nil && json.Unmarshal(b, &y) == nil && equalJSONValue(x, y)
}
