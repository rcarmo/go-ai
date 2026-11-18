package durable

import (
	"bufio"
	"compress/gzip"
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

// The source's raw commit checks claim and address ownership, not whether a
// conversation or task named by a record has already been stored.
func TestSourceV152SQLitePreservesRawOwnerLinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owners.sqlite")
	s, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	const raw = `[[{"type":"task","value":{"id":5,"conversationId":11,"kind":"t.two","version":1,"input":{"value":0},"state":{"status":"pending","checkpoint":{"phase":"ready"}},"background":false,"abortRequested":false}}],[{"type":"document.create","record":{"id":35,"kind":"b","key":"y","scope":{"kind":"conversation","conversationId":12},"history":"latest","fork":"current"},"content":{"kind":"base","version":1,"value":{"n":0,"at":0}}}],[{"type":"document.create","record":{"id":10,"kind":"b","scope":{"kind":"task","taskId":7}},"content":{"kind":"base","version":1,"value":{"n":0,"at":0}}}],[{"type":"submission","value":{"id":23,"conversationId":27,"type":"input","status":"queued"}}]]`
	var batches [][]StorageWrite
	if err := json.Unmarshal([]byte(raw), &batches); err != nil {
		t.Fatal(err)
	}
	var receipts []publicSQLiteReceipt
	r, err := s.Commit(bg, batches, func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) })
	if err != nil || r.Refused != nil || r.Stored != 4 || !reflect.DeepEqual(receipts, []publicSQLiteReceipt{{0, 1}, {1, 2}, {2, 3}, {3, 4}}) {
		t.Fatal(r, r.Refused.AsError(), receipts, err)
	}
	if err := s.Close(bg); err != nil {
		t.Fatal(err)
	}
	view, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close(bg)
	sub, found, err := view.Submission(bg, 23)
	if err != nil || !found || sub.Conversation != 27 || sub.Status != "queued" {
		t.Fatal(sub, found, err)
	}
	tsk, ok, err := view.Task(bg, 5)
	if err != nil || !ok || tsk.Conversation != 11 || tsk.State.Status != "pending" {
		t.Fatal(tsk, ok, err)
	}
	for _, want := range []struct {
		id    ID
		scope string
		owner ID
	}{{35, "conversation", 12}, {10, "task", 7}} {
		doc, ok, err := view.DocumentAt(bg, want.id, CurrentDocumentPoint())
		if err != nil || !ok || doc.Scope != want.scope || doc.Owner != want.owner || doc.Version != 1 || !equalJSONValue(doc.Value, JSON{"n": 0, "at": 0}) {
			t.Fatal(doc, ok, err, want)
		}
	}
}

func TestSourceV152SQLiteCreatesEmptyLifetimeInEitherOrder(t *testing.T) {
	for _, retirementFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(retirementFirst), func(t *testing.T) {
			s, err := OpenSQLite(filepath.Join(t.TempDir(), "empty.sqlite"), SQLiteOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(bg)
			seedRootConversation(t, s)
			create := StorageWrite{Type: "document.create", Record: &DocumentCreate{ID: 458, Kind: "a", Scope: DocumentCreateScope{Kind: "conversation", ConversationID: 315}, History: "latest", Fork: "current"}, Content: &DocumentContent{Kind: "base", Version: 1, Value: JSON{"n": 0, "at": 0}}}
			retire := StorageWrite{Type: "document.retire", ID: 458}
			part := []StorageWrite{create, retire}
			if retirementFirst {
				part[0], part[1] = part[1], part[0]
			}
			var receipts []publicSQLiteReceipt
			r, err := s.Commit(bg, [][]StorageWrite{part}, func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) })
			if err != nil || r.Refused != nil || r.Stored != 1 || !reflect.DeepEqual(receipts, []publicSQLiteReceipt{{0, 2}}) {
				t.Fatalf("empty lifetime: %+v %v %v", r, r.Refused.AsError(), err)
			}
			var created, retired, count int
			if err := s.db.QueryRowContext(bg, `SELECT created_at,retired_at,(SELECT count(*) FROM document_revisions WHERE document_id=458) FROM documents WHERE id=458`).Scan(&created, &retired, &count); err != nil || created != 2 || retired != 2 || count != 0 {
				t.Fatal(created, retired, count, err)
			}
			if doc, found, err := s.DocumentAt(bg, 458, CurrentDocumentPoint()); err != nil || found {
				t.Fatal(doc, found, err)
			}
			create.Record.ID = 459
			r, err = s.Commit(bg, [][]StorageWrite{{create}}, nil)
			if err != nil || r.Refused != nil || r.Stored != 1 {
				t.Fatal(r, r.Refused.AsError(), err)
			}
		})
	}
}

// Replay the original randomWrite/replacement stream, without adapting IDs,
// unsupported records, refused writes or the suffix after a refusal.
func TestSourceV152SQLiteSixSeedReceiptsAndTables(t *testing.T) {
	f, err := os.Open("testdata/pi-durable-v152/sqlite-random-receipts.jsonl.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	type round struct {
		Seed, Round int
		Batches     [][]StorageWrite
		Expected    struct {
			Stored   int
			Refused  bool
			Receipts [][2]uint64
		}
	}
	cases := make([][]round, 6)
	scanner := bufio.NewScanner(gz)
	for scanner.Scan() {
		var c round
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			t.Fatal(err)
		}
		if c.Seed < 1 || c.Seed > len(cases) || c.Round != len(cases[c.Seed-1]) {
			t.Fatal(c.Seed, c.Round)
		}
		cases[c.Seed-1] = append(cases[c.Seed-1], c)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	for i, rounds := range cases {
		t.Run(fmt.Sprint(i+1), func(t *testing.T) {
			if len(rounds) != 120 {
				t.Fatal(len(rounds))
			}
			dir := t.TempDir()
			open := func(name string) *SQLiteStorage {
				t.Helper()
				s, err := OpenSQLite(filepath.Join(dir, name), SQLiteOptions{})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = s.Close(bg) })
				seedRootConversation(t, s)
				return s
			}
			grouped, single := open("grouped.sqlite"), open("single.sqlite")
			var refusals, prefixes, groups int
			for _, c := range rounds {
				receipts := make([][2]uint64, 0)
				r, err := grouped.Commit(bg, c.Batches, func(i int, seq uint64) { receipts = append(receipts, [2]uint64{uint64(i), seq}) })
				if err != nil || r.Stored != c.Expected.Stored || (r.Refused != nil) != c.Expected.Refused || !reflect.DeepEqual(receipts, c.Expected.Receipts) {
					t.Fatalf("round %d: stored=%d refused=%v receipts=%v err=%v, want %+v", c.Round, r.Stored, r.Refused.AsError(), receipts, err, c.Expected)
				}
				for j, b := range c.Batches {
					r, err := single.Commit(bg, [][]StorageWrite{b}, nil)
					if err != nil {
						t.Fatalf("round %d part %d: %v", c.Round, j, err)
					}
					if r.Stored == 0 {
						if j != c.Expected.Stored || r.Refused == nil {
							t.Fatalf("round %d part %d: unexpected single result %+v", c.Round, j, r)
						}
						break
					}
				}
				compareRandomSQLiteTables(t, c.Round, grouped, single)
				if c.Expected.Refused {
					refusals++
					if c.Expected.Stored > 0 {
						prefixes++
					}
				} else if len(c.Batches) > 1 {
					groups++
				}
			}
			if refusals <= 10 || prefixes <= 10 || groups <= 10 {
				t.Fatal(refusals, prefixes, groups)
			}
		})
	}
}

func compareRandomSQLiteTables(t *testing.T, round int, grouped, single *SQLiteStorage) {
	t.Helper()
	read := func(s *SQLiteStorage, query string) [][]any {
		t.Helper()
		rows, err := s.db.QueryContext(bg, query)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			t.Fatal(err)
		}
		var all [][]any
		for rows.Next() {
			values := make([]any, len(columns))
			targets := make([]any, len(columns))
			for i := range targets {
				targets[i] = &values[i]
			}
			if err := rows.Scan(targets...); err != nil {
				t.Fatal(err)
			}
			all = append(all, values)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		return all
	}
	for _, table := range []string{"durable_metadata", "record_ids", "conversations", "entries", "tasks", "submissions", "documents", "document_revisions"} {
		q := "SELECT * FROM " + table + " ORDER BY 1, 2"
		a, b := read(grouped, q), read(single, q)
		if !reflect.DeepEqual(a, b) {
			t.Fatalf("round %d grouped %s differs from separately committed table (%d vs %d rows)", round, table, len(a), len(b))
		}
	}
	stale := read(grouped, `SELECT revision.document_id, revision.seq FROM document_revisions AS revision
 JOIN documents AS document ON document.id = revision.document_id
 WHERE (document.scope_kind != 'conversation' OR json_extract(document.record, '$.history') = 'latest')
 AND (document.retired_at IS NOT NULL OR revision.seq < (
   SELECT MAX(seq) FROM document_revisions WHERE document_id = revision.document_id AND kind = 'base'))`)
	if len(stale) != 0 {
		t.Fatalf("round %d has stale current-only rows: %v", round, stale)
	}
}
