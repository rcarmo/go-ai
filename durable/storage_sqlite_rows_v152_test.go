package durable

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// sqlite-commit.test.ts groupOf, on the default source-layout backend.
func TestSourceV152SQLiteInsertPacking(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    []string
		params  []int
		compact []bool
	}{
		{"100 compact", repeatedRows(100, strings.Repeat("x", 7_000)), []int{1, 1}, []bool{true, true}},
		{"2 quoted", repeatedRows(2, strings.Repeat(`"`, 400_000)), []int{10}, []bool{false}},
		{"19 quoted", repeatedRows(19, strings.Repeat(`"`, 10_000)), []int{80, 10, 5}, []bool{false, false, false}},
		// Supplemental: the source measures UTF-16 units and pools small rows even
		// when a large row separates them. Byte count is not a valid substitute.
		{"mixed ASCII", []string{"small before", strings.Repeat("x", 10_000), "small after"}, []int{1, 5}, []bool{true, false}},
		{"mixed Unicode", []string{strings.Repeat("𐐀", 3_500), strings.Repeat("x", 10_000), strings.Repeat("界", 7_000)}, []int{1, 5}, []bool{true, false}},
		{"Unicode compact array", repeatedRows(39, strings.Repeat("界", 7_000)), []int{1}, []bool{true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := DefaultLimits()
			limits.MaxRecordBytes = 1 << 20
			limits.MaxStringBytes = 512 << 10
			s, err := OpenSQLite(filepath.Join(t.TempDir(), "store.sqlite"), SQLiteOptions{Limits: &limits})
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(bg)
			setup := []StorageWrite{{Type: "conversation", Value: Conversation{ID: 1}}}
			for i := range tc.data {
				id := mint(t, s)
				key := fmt.Sprint(id)
				if id != ID(i+2) {
					t.Fatal(id)
				}
				setup = append(setup, v152SessionDocument(id, "counter", &key, JSON{"n": 0}))
			}
			commitRows := func(batches [][]StorageWrite, receive func(int, uint64)) {
				t.Helper()
				r, err := s.Commit(bg, batches, receive)
				if err != nil || r.Refused != nil || r.Stored != len(batches) {
					t.Fatalf("stored=%d refusal=%v error=%v", r.Stored, r.Refused.AsError(), err)
				}
			}
			commitRows([][]StorageWrite{setup}, nil)
			batches := make([][]StorageWrite, len(tc.data))
			entryIDs := make([]ID, len(batches))
			for i, data := range tc.data {
				id := mint(t, s)
				entryIDs[i] = id
				batches[i] = []StorageWrite{{Type: "document.change", ID: ID(i + 2), Content: &DocumentContent{Kind: "delta", Version: 1, Ops: []Operation{{"s", []any{"n"}, 1}}}}, {Type: "entry", Value: Entry{ID: id, Conversation: 1, Kind: "note", HasData: true, Data: data}}}
			}
			recording := &recordingSQLiteConnection{sqliteConnection: s.db}
			s.db = recording
			var receipts []publicSQLiteReceipt
			commitRows(batches, func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) })
			if tc.name == "100 compact" {
				count := 0
				for _, call := range recording.calls {
					if call.text != "BEGIN IMMEDIATE" && call.text != "COMMIT" {
						count++
					}
				}
				if count != 9 {
					t.Fatalf("compact group ran %d transaction statements, want 9", count)
				}
			}
			var params []int
			var compact []bool
			for _, call := range recording.calls {
				if !strings.HasPrefix(call.text, `INSERT INTO "entries"`) {
					continue
				}
				packed := strings.Contains(call.text, "json_each")
				params = append(params, len(call.args))
				compact = append(compact, packed)
				if packed {
					raw := call.args[0].(string)
					units := 0
					for _, r := range raw {
						units++
						if r > 0xffff {
							units++
						}
					}
					if units > 512*1024 {
						t.Fatalf("array has %d UTF-16 units", units)
					}
				}
			}
			if !reflect.DeepEqual(params, tc.params) || !reflect.DeepEqual(compact, tc.compact) {
				t.Fatalf("entries parameters=%v compact=%v; want %v / %v", params, compact, tc.params, tc.compact)
			}
			s.db = recording.sqliteConnection
			for i, id := range entryIDs {
				if receipts[i] != (publicSQLiteReceipt{i, uint64(i + 2)}) {
					t.Fatal(receipts)
				}
				var raw string
				if err := s.db.QueryRowContext(bg, "SELECT record FROM entries WHERE id=?", id).Scan(&raw); err != nil {
					t.Fatal(err)
				}
				var record Entry
				if err := json.Unmarshal([]byte(raw), &record); err != nil || record.Data != tc.data[i] {
					t.Fatalf("record %d changed data: %v", id, err)
				}
			}
		})
	}
}

func repeatedRows(n int, value string) []string {
	rows := make([]string, n)
	for i := range rows {
		rows[i] = value
	}
	return rows
}

// PendingWrites keeps one task row per ID. Pooling compact rows ahead of
// bound rows must not resurrect a superseded task with a large checkpoint.
func TestSourceV152SQLiteCoalescesTaskRowsBeforePacking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.sqlite")
	s, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	task := TaskRecord{ID: 2, Conversation: 1, Kind: "test.task", Version: 1, Input: &TaskValue{Present: true, Value: nil}, State: TaskState{Status: "pending", Checkpoint: JSON{"text": strings.Repeat("x", 10_000)}}}
	running := task
	running.State = TaskState{Status: "running", Checkpoint: JSON{"text": "last"}}
	recording := &recordingSQLiteConnection{sqliteConnection: s.db}
	s.db = recording
	r, err := s.Commit(bg, [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}, {Type: "task", Value: task}}, {{Type: "task", Value: running}}}, nil)
	if err != nil || r.Refused != nil || r.Stored != 2 {
		t.Fatalf("stored=%d refusal=%v error=%v", r.Stored, r.Refused.AsError(), err)
	}
	var params []int
	for _, call := range recording.calls {
		if strings.HasPrefix(call.text, `INSERT INTO "tasks"`) {
			params = append(params, len(call.args))
		}
	}
	if !reflect.DeepEqual(params, []int{1}) {
		t.Fatalf("task writes have parameter counts %v, want a single compact row", params)
	}
	if err := s.Close(bg); err != nil {
		t.Fatal(err)
	}
	s, err = OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	got, found, err := s.Task(bg, 2)
	if err != nil || !found || got.State.Status != "running" || !equalJSONValue(got.State.Checkpoint, JSON{"text": "last"}) {
		t.Fatal(got, found, err)
	}
}

// Supplemental to the pinned database-error identity cases: fail a statement
// after earlier table writes, then retry on the same writer after rollback.
type upstreamWriteFault struct {
	sqliteConnection
	err error
}

func (f *upstreamWriteFault) ExecContext(ctx context.Context, text string, args ...any) (sql.Result, error) {
	if strings.HasPrefix(text, `INSERT INTO "entries"`) {
		return nil, f.err
	}
	return f.sqliteConnection.ExecContext(ctx, text, args...)
}

func TestSourceV152SQLiteDatabaseWriteErrorIdentity(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "fault.sqlite"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	raw := s.db
	cause := errors.New("test database write failure")
	s.db = &upstreamWriteFault{sqliteConnection: raw, err: cause}
	batches := [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}}, {{Type: "entry", Value: Entry{ID: 2, Conversation: 1, Kind: "note"}}}}
	var seqs []uint64
	receive := func(_ int, seq uint64) { seqs = append(seqs, seq) }
	r, err := s.Commit(bg, batches, receive)
	if err != cause || r.Stored != 0 || r.Refused != nil || len(seqs) != 0 {
		t.Fatalf("want original write error, zero receipts, no refusal; got result=%+v receipts=%v err=%v", r, seqs, err)
	}
	s.db = raw
	if _, found, err := s.Conversation(bg, 1); err != nil || found {
		t.Fatal(found, err)
	}
	r, err = s.Commit(bg, batches, receive)
	if err != nil || r.Stored != 2 || r.Refused != nil || !reflect.DeepEqual(seqs, []uint64{1, 2}) {
		t.Fatal(r, seqs, err)
	}
}

type upstreamCheckFault struct {
	sqliteConnection
	cause error
}

func (f *upstreamCheckFault) QueryContext(ctx context.Context, text string, args ...any) (*sql.Rows, error) {
	if strings.Contains(text, `FROM "record_ids"`) {
		return nil, f.cause
	}
	return f.sqliteConnection.QueryContext(ctx, text, args...)
}

func TestSourceV152SQLiteDatabaseCheckReadErrorIdentity(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "check.sqlite"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	raw := s.db
	cause := errors.New("test ID check read failure")
	s.db = &upstreamCheckFault{sqliteConnection: raw, cause: cause}
	batches := [][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}}, {{Type: "entry", Value: Entry{ID: 2, Conversation: 1, Kind: "note"}}}}
	var receipts []publicSQLiteReceipt
	receive := func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) }
	got, err := s.Commit(bg, batches, receive)
	if err != cause || got.Stored != 0 || got.Refused != nil || len(receipts) != 0 {
		t.Fatalf("want original check-read error and no receipts: result=%+v receipts=%v error=%v", got, receipts, err)
	}
	s.db = raw
	got, err = s.Commit(bg, batches, receive)
	if err != nil || got.Refused != nil || got.Stored != 2 || !reflect.DeepEqual(receipts, []publicSQLiteReceipt{{0, 1}, {1, 2}}) {
		t.Fatal(got, receipts, err)
	}
}

func TestSourceV152SQLiteDeltaDoesNotAppendAgainstStaleVersion(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "changed.sqlite"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	r, err := s.Commit(bg, [][]StorageWrite{{v152SessionDocument(2, "counter", nil, JSON{"n": 0})}}, nil)
	if err != nil || r.Refused != nil || r.Stored != 1 {
		t.Fatal(r, err)
	}
	// Local SQL fault: modify a touched row without changing allocator metadata.
	if err := s.exec(`UPDATE document_revisions SET version=2 WHERE document_id=2`); err != nil {
		t.Fatal(err)
	}
	received := false
	r, err = s.Commit(bg, [][]StorageWrite{{{Type: "document.change", ID: 2, Content: &DocumentContent{Kind: "delta", Version: 1, Ops: []Operation{{"s", []any{"n"}, 1}}}}}}, func(int, uint64) { received = true })
	if err == nil || err.Error() != "upstream SQLite document version changed externally" || r.Refused != nil || r.Stored != 0 || received {
		t.Fatalf("want stale-version rollback: result=%+v received=%v error=%v", r, received, err)
	}
	var revisions int
	if err := s.db.QueryRowContext(bg, `SELECT count(*) FROM document_revisions WHERE document_id=2`).Scan(&revisions); err != nil || revisions != 1 {
		t.Fatal(revisions, err)
	}
}

// The second sqlite-commit.test.ts workload exercises every check key and
// mutation shape in the same call, including address reuse and revision drop.
func TestSourceV152SQLiteReadsAndWritesThroughIndexes(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "indexed.sqlite"), SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close(bg)
	first, second := mint(t, s), mint(t, s)
	create := func(id ID, value JSON) StorageWrite {
		return StorageWrite{Type: "document.create", Record: &DocumentCreate{ID: id, Kind: "plan", Scope: DocumentCreateScope{Kind: "conversation", ConversationID: 1}, History: "latest", Fork: "current"}, Content: &DocumentContent{Kind: "base", Version: 1, Value: value}}
	}
	commit := func(parts [][]StorageWrite, receive func(int, uint64)) {
		t.Helper()
		r, err := s.Commit(bg, parts, receive)
		if err != nil || r.Refused != nil || r.Stored != len(parts) {
			t.Fatalf("commit: %+v / %v", r, err)
		}
	}
	commit([][]StorageWrite{{{Type: "conversation", Value: Conversation{ID: 1}}}, {create(first, JSON{"n": 0})}}, nil)
	entry := mint(t, s)
	recorder := &recordingSQLiteConnection{sqliteConnection: s.db}
	s.db = recorder
	var receipts []publicSQLiteReceipt
	commit([][]StorageWrite{
		{{Type: "document.change", ID: first, Content: &DocumentContent{Kind: "delta", Version: 1, Ops: []Operation{{"s", []any{"n"}, 1}}}}},
		{{Type: "document.change", ID: first, Content: &DocumentContent{Kind: "base", Version: 1, Value: JSON{"n": 2}}}, {Type: "entry", Value: Entry{ID: entry, Conversation: 1, Kind: "note"}}},
		{{Type: "document.retire", ID: first}, create(second, JSON{})},
	}, func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) })
	if !reflect.DeepEqual(receipts, []publicSQLiteReceipt{{0, 3}, {1, 4}, {2, 5}}) {
		t.Fatal(receipts)
	}
	checks := []string{`FROM "record_ids"`, `FROM "documents" WHERE id`, `ORDER BY seq DESC LIMIT 1`, `JOIN "documents" AS document`, `INSERT INTO "record_ids"`, `INSERT INTO "entries"`, `INSERT INTO "documents"`, `DELETE FROM "document_revisions"`, `INSERT INTO "document_revisions"`}
	count := 0
	for _, call := range recorder.calls {
		if !strings.Contains(call.text, "json_each") {
			continue
		}
		count++
		checkCompactFramePlan(t, recorder.sqliteConnection, call)
		for i, check := range checks {
			if check != "" && strings.Contains(call.text, check) {
				checks[i] = ""
			}
		}
	}
	if count <= 5 {
		t.Fatalf("only %d batched statements", count)
	}
	for _, check := range checks {
		if check != "" {
			t.Fatalf("did not exercise %s", check)
		}
	}
}
