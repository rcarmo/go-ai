package durable

import (
	"encoding/json"
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
