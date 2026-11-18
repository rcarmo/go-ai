package durable

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type recordedSQLiteCall struct {
	text string
	args []any
}

type recordingSQLiteConnection struct {
	sqliteConnection
	calls []recordedSQLiteCall
}

func (r *recordingSQLiteConnection) ExecContext(ctx context.Context, text string, args ...any) (sql.Result, error) {
	r.calls = append(r.calls, recordedSQLiteCall{text, slices.Clone(args)})
	return r.sqliteConnection.ExecContext(ctx, text, args...)
}
func (r *recordingSQLiteConnection) QueryContext(ctx context.Context, text string, args ...any) (*sql.Rows, error) {
	r.calls = append(r.calls, recordedSQLiteCall{text, slices.Clone(args)})
	return r.sqliteConnection.QueryContext(ctx, text, args...)
}
func (r *recordingSQLiteConnection) QueryRowContext(ctx context.Context, text string, args ...any) *sql.Row {
	r.calls = append(r.calls, recordedSQLiteCall{text, slices.Clone(args)})
	return r.sqliteConnection.QueryRowContext(ctx, text, args...)
}

type publicSQLiteReceipt struct {
	index int
	seq   uint64
}

func v152SessionDocument(id ID, kind string, key *string, value JSON) StorageWrite {
	return StorageWrite{Type: "document.create", Record: &DocumentCreate{ID: id, Kind: kind, Key: key, Scope: DocumentCreateScope{Kind: "session"}}, Content: &DocumentContent{Kind: "base", Version: 1, Value: value}}
}

func TestSourceV152SQLiteRefusesThirdIncarnationAfterReplacement(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		name := "creation first"
		if reverse {
			name = "retirement first"
		}
		t.Run(name, func(t *testing.T) {
			sqliteStoreFresh(t, func(t *testing.T, f sqliteFixture) {
				create := func(id ID) StorageWrite { return v152SessionDocument(id, "slot", nil, JSON{}) }
				first, err := f.store.Commit(bg, [][]StorageWrite{{create(10)}}, nil)
				if err != nil || first.Stored != 1 || first.Refused != nil {
					t.Fatal(first, err)
				}
				replace := []StorageWrite{create(11), {Type: "document.retire", ID: 10}}
				if reverse {
					slices.Reverse(replace)
				}
				var receipts []publicSQLiteReceipt
				result, err := f.store.Commit(bg, [][]StorageWrite{replace, {create(12)}, {v152SessionDocument(13, "not-stored", nil, JSON{})}}, func(i int, seq uint64) {
					receipts = append(receipts, publicSQLiteReceipt{i, seq})
				})
				refused, ok := result.Refused.Value().(*StorageRejected)
				if err != nil || result.Stored != 1 || !ok || refused.Reason != "Document address already has a current incarnation" {
					t.Fatalf("replacement refusal = %d, %v, %v", result.Stored, result.Refused.Value(), err)
				}
				if !reflect.DeepEqual(receipts, []publicSQLiteReceipt{{0, 2}}) {
					t.Fatalf("receipts = %v", receipts)
				}
				for _, store := range []func() Storage{func() Storage { return f.store }, func() Storage { return f.reopen() }} {
					s := store()
					point := CurrentDocumentPoint()
					current, found, err := s.FindDocument(bg, DocumentAddress{Scope: "session", Kind: "slot"}, point)
					if err != nil || !found || current.ID != 11 {
						t.Fatal(current, found, err)
					}
					for _, id := range []ID{10, 12, 13} {
						if value, found, err := s.DocumentAt(bg, id, point); err != nil || found {
							t.Fatal(id, value, found, err)
						}
					}
				}
			})
		})
	}
}

// The frame-backed schema has a different SQL layout from upstream. Count
// real connection calls, including BEGIN/COMMIT, without requiring its eight
// statements over tables that do not yet exist in the native schema.
func TestSourceV152SQLiteSmallGroupStatementCount(t *testing.T) {
	var three, many []string
	for _, count := range []int{1, 3, 150} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			calls := v152SQLiteCommitGroup(t, count, 0)
			statements := make([]string, len(calls))
			for i, call := range calls {
				statements[i] = call.text
			}
			if count == 3 {
				three = statements
			}
			if count == 150 {
				many = statements
			}
			if len(statements) != 3 {
				t.Errorf("group of %d used %d SQL statements; want 3", count, len(statements))
			}
		})
	}
	if !slices.Equal(three, many) {
		t.Error("compact groups of 3 and 150 did not execute the same SQL text")
	}
}

func TestSourceV152SQLiteCompactChunksAndBoundRows(t *testing.T) {
	t.Run("compact arrays split at 512 KiB", func(t *testing.T) {
		calls := v152SQLiteCommitGroup(t, 100, strings.Repeat("x", 7_000))
		if len(calls) != 4 {
			t.Fatalf("compact group used %d statements; want 4", len(calls))
		}
		for _, call := range calls[1:3] {
			if !strings.Contains(call.text, "json_each") || len(call.args) != 1 {
				t.Fatalf("unexpected compact insert: %s, %d parameters", call.text, len(call.args))
			}
			if arg, ok := call.args[0].(string); !ok || len(arg) > 512*1024 {
				t.Fatalf("compact argument: %T (%d bytes)", call.args[0], len(arg))
			}
		}
	})
	for _, tc := range []struct {
		count, quotes int
		parameters    []int
	}{
		{2, 400_000, []int{8}},
		{19, 10_000, []int{64, 8, 4}},
	} {
		t.Run(fmt.Sprintf("bound rows %d", tc.count), func(t *testing.T) {
			calls := v152SQLiteCommitGroup(t, tc.count, strings.Repeat("\"", tc.quotes))
			var parameters []int
			for _, call := range calls[1 : len(calls)-1] {
				if strings.Contains(call.text, "json_each") {
					t.Fatalf("large rows used JSON transport: %s", call.text)
				}
				parameters = append(parameters, len(call.args))
			}
			if !slices.Equal(parameters, tc.parameters) {
				t.Fatalf("parameters = %v, want %v", parameters, tc.parameters)
			}
		})
	}
}

func v152SQLiteCommitGroup(t *testing.T, count int, data any) []recordedSQLiteCall {
	t.Helper()
	var calls []recordedSQLiteCall
	limits := DefaultLimits()
	limits.MaxStringBytes = 512 << 10
	limits.MaxRecordBytes = 1 << 20
	sqliteStoreFresh(t, func(t *testing.T, f sqliteFixture) {
		store := f.store.(*SQLiteStorage)
		setup := []StorageWrite{{Type: "conversation", Value: Conversation{ID: 1}}}
		documents := make([]ID, count)
		for i := range documents {
			id := mint(t, store)
			key := fmt.Sprint(id)
			documents[i] = id
			setup = append(setup, v152SessionDocument(id, "counter", &key, JSON{"n": 0}))
		}
		created, err := store.Commit(bg, [][]StorageWrite{setup}, nil)
		if err != nil || created.Stored != 1 || created.Refused != nil {
			t.Fatal(created, err)
		}
		batches := make([][]StorageWrite, len(documents))
		entries := make([]ID, count)
		for i, id := range documents {
			entries[i] = mint(t, store)
			batches[i] = []StorageWrite{
				{Type: "document.change", ID: id, Content: &DocumentContent{Kind: "delta", Version: 1, Ops: []Operation{{"s", []any{"n"}, 1}}}},
				{Type: "entry", Value: Entry{ID: entries[i], Conversation: 1, Kind: "note", HasData: true, Data: data}},
			}
		}
		recorder := &recordingSQLiteConnection{sqliteConnection: store.db}
		store.db = recorder
		var receipts []publicSQLiteReceipt
		result, err := store.Commit(bg, batches, func(i int, seq uint64) { receipts = append(receipts, publicSQLiteReceipt{i, seq}) })
		calls = slices.Clone(recorder.calls)
		if err != nil || result.Stored != count || result.Refused != nil || len(receipts) != count {
			t.Fatal(result.Stored, result.Refused.Value(), err, len(receipts))
		}
		for _, call := range calls {
			if strings.Contains(call.text, "json_each") {
				checkCompactFramePlan(t, recorder.sqliteConnection, call)
			}
		}
		reopened := f.reopen()
		for i, receipt := range receipts {
			want := publicSQLiteReceipt{i, uint64(i) + 2}
			if receipt != want {
				t.Fatalf("receipt %d: %v, want %v", i, receipt, want)
			}
			entry, found, err := reopened.Entry(bg, entries[i])
			if err != nil || !found || entry.CommitSeq != want.seq || !equalJSONValue(entry.Entry.Data, data) {
				t.Fatalf("entry %d: seq=%d, found=%v, error=%v, matches data=%v", i, entry.CommitSeq, found, err, equalJSONValue(entry.Entry.Data, data))
			}
			doc, found, err := reopened.DocumentAt(bg, documents[i], CurrentDocumentPoint())
			if err != nil || !found || !equalJSONValue(doc.Value, JSON{"n": 1}) {
				t.Fatal(i, doc, found, err)
			}
		}
	}, limits)
	return calls
}

func checkCompactFramePlan(t *testing.T, db sqliteConnection, call recordedSQLiteCall) {
	t.Helper()
	rows, err := db.QueryContext(bg, "EXPLAIN QUERY PLAN "+call.text, call.args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent, aux int
		var detail string
		if err := rows.Scan(&id, &parent, &aux, &detail); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(detail, "SCAN") && !strings.Contains(detail, "VIRTUAL TABLE") {
			t.Fatalf("compact insert scans a stored table: %s", detail)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestSourceV152SQLiteCompactGroupRetainsEscapedData(t *testing.T) {
	calls := v152SQLiteCommitGroup(t, 3, "quoted \"text\" and \x00 \u2028 😀")
	if len(calls) != 3 || !strings.Contains(calls[1].text, "json_each") {
		t.Fatal("escaped input did not exercise compact frame transport")
	}
}
