package durable

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Literal prefix of the seed-6 upstream corpus. The raw storage boundary
// accepts records with unresolved conversation/head IDs; Tx still validates
// the relationships it owns. Do not tailor the generated writes to Go.
func TestSourceV152SQLiteSeedSixPrefix(t *testing.T) {
	const fixture = "testdata/pi-durable-v152/sqlite-seed6-first-rounds.jsonl"
	f, err := os.Open(fixture)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	path := filepath.Join(t.TempDir(), "seed.sqlite")
	s, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	seedRootConversation(t, s)
	scanner := bufio.NewScanner(f)
	round := 0
	for scanner.Scan() {
		var c struct {
			Seed, Round int
			Batches     [][]StorageWrite
			Expected    struct {
				Stored   int
				Refused  bool
				Receipts [][2]uint64
			}
		}
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		if c.Seed != 6 || c.Round != round {
			t.Fatal(c.Seed, c.Round, round)
		}
		receipts := make([][2]uint64, 0)
		r, err := s.Commit(bg, c.Batches, func(i int, seq uint64) { receipts = append(receipts, [2]uint64{uint64(i), seq}) })
		if err != nil || r.Stored != c.Expected.Stored || (r.Refused != nil) != c.Expected.Refused || !reflect.DeepEqual(receipts, c.Expected.Receipts) {
			t.Fatalf("round %d: stored=%d refused=%v receipts=%v err=%v, want %+v", round, r.Stored, r.Refused.AsError(), receipts, err, c.Expected)
		}
		round++
	}
	if err := scanner.Err(); err != nil || round != 3 {
		t.Fatal(round, err)
	}
	if err := s.Close(bg); err != nil {
		t.Fatal(err)
	}
	view, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close(bg)
	for _, want := range []Entry{{ID: 7, Conversation: 3, Kind: "note", HasData: true, Data: 771}, {ID: 8, Conversation: 1, Kind: "note", HasData: true, Data: 533}, {ID: 9, Conversation: 1, Kind: "note", HasData: true, Data: 741, Head: 4}} {
		got, found, err := view.Entry(bg, want.ID)
		if err != nil || !found || got.CommitSeq != 3 || got.Entry.ID != want.ID || got.Entry.Conversation != want.Conversation || got.Entry.Kind != want.Kind || got.Entry.Head != want.Head || !got.Entry.HasData || !equalJSONValue(got.Entry.Data, want.Data) {
			t.Fatal(got, found, err, want)
		}
	}
}

// sqlite-storage.test.ts check-read fault recovery: a logical refusal after
// a valid part retains the source's precise ID diagnostic and receipt.
func TestSourceV152SQLiteIDRefusalKeepsPrefix(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "claims.sqlite"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	seedRootConversation(t, s)
	first, after := mint(t, s), mint(t, s)
	entry := func(id ID) StorageWrite {
		return StorageWrite{Type: "entry", Value: Entry{ID: id, Conversation: 1, Kind: "note"}}
	}
	var receipts []publicSQLiteReceipt
	r, err := s.Commit(bg, [][]StorageWrite{{entry(first)}, {{Type: "conversation", Value: Conversation{ID: 1}}}, {entry(after)}}, func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) })
	var refused *StorageRejected
	if err != nil || r.Stored != 1 || !errors.As(r.Refused.AsError(), &refused) || refused.Reason != "ID 1 already belongs to conversation" || !reflect.DeepEqual(receipts, []publicSQLiteReceipt{{0, 2}}) {
		t.Fatalf("result=%+v refusal=%v receipts=%v err=%v", r, r.Refused.AsError(), receipts, err)
	}
	if got, found, err := s.Entry(bg, first); err != nil || !found || got.CommitSeq != 2 || got.Entry.Kind != "note" {
		t.Fatal(got, found, err)
	}
	if got, found, err := s.Entry(bg, after); err != nil || found {
		t.Fatal(got, found, err)
	}
	r, err = s.Commit(bg, [][]StorageWrite{{entry(after), entry(after)}}, nil)
	if err != nil || r.Stored != 0 || !errors.As(r.Refused.AsError(), &refused) || refused.Reason != fmt.Sprintf("ID %d is written more than once", after) {
		t.Fatal(r.Refused.AsError(), err)
	}
}

// An accepted terminal task in the source random corpus has an opaque outcome.
// Storage preserves its JSON without pretending it is a typed runtime result.
func TestSourceV152SQLiteStoresOpaqueTerminalOutcome(t *testing.T) {
	const wire = `{"type":"task","value":{"id":33,"conversationId":11,"kind":"t.one","version":1,"input":{"value":4},"state":{"status":"terminal","outcome":{"kind":"returned","value":1}},"background":false,"abortRequested":false}}`
	var write StorageWrite
	if err := json.Unmarshal([]byte(wire), &write); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "task.sqlite")
	s, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	r, err := s.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 11}}, write}}, nil)
	if err != nil || r.Refused != nil || r.Stored != 1 {
		t.Fatal(r, r.Refused.AsError(), err)
	}
	if err := s.Close(bg); err != nil {
		t.Fatal(err)
	}
	view, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close(bg)
	task, found, err := view.Task(bg, 33)
	if err != nil || !found || task.State.Status != "terminal" || task.State.Outcome != nil {
		t.Fatal(task, found, err)
	}
	got, err := json.Marshal(StorageWrite{Type: "task", Value: task})
	if err != nil {
		t.Fatal(err)
	}
	var original, encoded any
	if err := decodePublicJSON([]byte(wire), &original); err != nil {
		t.Fatal(err)
	}
	if err := decodePublicJSON(got, &encoded); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(encoded, original) {
		t.Fatalf("round trip changed input: %s", got)
	}
	var state TaskState
	if err := json.Unmarshal([]byte(`{"status":"terminal","outcome":{"kind":"returned","value":1}}`), &state); err == nil {
		t.Fatal("opaque data became a typed runtime outcome")
	}
}
