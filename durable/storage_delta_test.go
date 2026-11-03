package durable

import (
	"bytes"
	"errors"

	"path/filepath"
	"reflect"
	"testing"
)

func deltaWrite(id ID, version uint64, ops ...Operation) Write {
	return Write{Op: "delta-document", Delta: &DocumentDelta{ID: id, Version: version, Ops: ops}}
}
func TestStorageDeltaHistoryCountVersionAndCopyReopen(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s := b.store
		id := mint(t, s)
		d := historyDocument(id, 1, "delta.history", "", "rewindable", "asOf")
		d.Value = JSON{"xs": []any{}}
		base := apply(t, s, putDocument(d))
		points := []uint64{base}
		for i := 0; i < 8; i++ {
			points = append(points, apply(t, s, deltaWrite(id, 1, Operation{"p", []any{"xs"}, i, 0, []any{i}})))
		}
		verify := func(s Storage) {
			h := documentStore(t, s)
			for i, seq := range points {
				got, ok, err := h.DocumentAt(bg, id, DocumentPoint{Seq: seq})
				if err != nil || !ok || got.DeltasSinceBase != uint64(i) || len(got.Value["xs"].([]any)) != i {
					t.Fatal("historical tail/count", i, got, err)
				}
			}
			got, ok, err := h.DocumentAt(bg, id, CurrentDocumentPoint())
			if err != nil || !ok || got.DeltasSinceBase != 8 {
				t.Fatal("current tail", got, err)
			}
			snapshot := snap(t, s)
			snapshot.DocumentRevisions[id][1].Ops[0][4].([]any)[0] = 99
			got, _, err = h.DocumentAt(bg, id, CurrentDocumentPoint())
			if err != nil || got.Value["xs"].([]any)[0].(interface{ String() string }).String() != "0" {
				t.Fatal("detached revision ops", err)
			}
		}
		verify(s)
		s = b.reopen()
		verify(s)
		copyID := mint(t, s)
		target := d
		target.ID = copyID
		target.Kind = "delta.history"
		target.Owner = 1
		target.Family = true
		target.Key = "copy"
		target.Value = nil
		target.Version = 0
		// Exact address metadata must match source, so create a child conversation.
		child := mint(t, s)
		apply(t, s, Write{Op: "create-conversation", Conversation: &Conversation{ID: child}})
		target.Owner = child
		target.Family = false
		target.Key = ""
		apply(t, s, Write{Op: "copy-document", Document: &target, Source: &DocumentCopySource{ID: id, At: CurrentDocumentPoint()}})
		got, ok, err := documentStore(t, s).DocumentAt(bg, copyID, CurrentDocumentPoint())
		if err != nil || !ok || got.DeltasSinceBase != 0 || len(got.Value["xs"].([]any)) != 8 {
			t.Fatal("copy materialisation/reset", got, err)
		}
		d.Version = 2
		d.Value = JSON{"xs": []any{"v2"}}
		migration := apply(t, s, putDocument(d))
		apply(t, s, deltaWrite(id, 2, Operation{"a", []any{"xs", 0}, "!"}))
		got, _, err = documentStore(t, s).DocumentAt(bg, id, DocumentPoint{Seq: migration})
		if err != nil || got.Version != 2 || got.DeltasSinceBase != 0 {
			t.Fatal("version base reset", got, err)
		}
		got, _, err = documentStore(t, s).DocumentAt(bg, id, CurrentDocumentPoint())
		if err != nil || got.DeltasSinceBase != 1 || got.Value["xs"].([]any)[0] != "v2!" {
			t.Fatal(got, err)
		}
		s = b.reopen()
		got, _, err = documentStore(t, s).DocumentAt(bg, id, CurrentDocumentPoint())
		if err != nil || got.DeltasSinceBase != 1 {
			t.Fatal("version reopen", got, err)
		}
	})
}
func TestStorageDeltaRejectionAtomicAndLatestTail(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s := b.store
		id := mint(t, s)
		d := Document{ID: id, Scope: "session", Kind: "delta.latest", Version: 1, Value: JSON{"s": "abc"}}
		apply(t, s, putDocument(d))
		apply(t, s, deltaWrite(id, 1, Operation{"a", []any{"s"}, "d"}))
		before := snap(t, s)
		for _, w := range []Write{
			deltaWrite(id, 2, Operation{"a", []any{"s"}, "x"}), deltaWrite(ID(1), 1, Operation{"r", JSON{}}), deltaWrite(id, 1),
			deltaWrite(id, 1, Operation{"r", []any{}}), deltaWrite(id, 1, Operation{"s", []any{"bad", "x"}, 1}),
			{Op: "put-document", Document: &d, Delta: &DocumentDelta{ID: id, Version: 1, Ops: []Operation{{"r", JSON{}}}}},
		} {
			if _, err := s.Apply(bg, Batch{Writes: []Write{w}}); err == nil {
				t.Fatal("invalid delta admitted")
			}
			if !reflect.DeepEqual(before, snap(t, s)) {
				t.Fatal("rejected delta changed state")
			}
		}
		if _, _, err := documentStore(t, s).DocumentAt(bg, id, DocumentPoint{Seq: before.Seq}); err == nil {
			t.Fatal("latest-only historical read")
		}
		d.Value = JSON{"s": "reset"}
		apply(t, s, putDocument(d))
		apply(t, s, deltaWrite(id, 1, Operation{"t", []any{"s"}, 1}))
		s = b.reopen()
		got, ok, err := documentStore(t, s).DocumentAt(bg, id, CurrentDocumentPoint())
		if err != nil || !ok || got.DeltasSinceBase != 1 || got.Value["s"] != "eset" {
			t.Fatal("latest tail/reset/reopen", got, err)
		}
		apply(t, s, retireDocument(d))
		if _, err = s.Apply(bg, Batch{Writes: []Write{deltaWrite(id, 1, Operation{"r", JSON{}})}}); err == nil {
			t.Fatal("retired delta")
		}
	})
}
func TestStorageDeltaBudgetAndSourceFence(t *testing.T) {
	for _, name := range []string{"sqlite"} {
		t.Run(name, func(t *testing.T) {
			l := DefaultLimits()
			l.MaxDocumentBytes = 512
			l.MaxRecordBytes = 2048
			l.MaxStringBytes = 1024
			var s Storage
			var err error

			s, err = openSQLiteTestDirectory(filepath.Join(t.TempDir(), "private"), SQLiteOptions{Limits: &l})

			if err != nil {
				t.Fatal(err)
			}
			defer s.Close(bg)
			id := mint(t, s)
			d := historyDocument(id, 1, "delta.budget", "", "rewindable", "current")
			d.Value = JSON{"s": ""}
			apply(t, s, putDocument(d))
			body := string(make([]byte, 0))
			for i := 0; i < 504; i++ {
				body += "x"
			}
			apply(t, s, deltaWrite(id, 1, Operation{"a", []any{"s"}, body}))
			before := snap(t, s)
			if _, err = s.Apply(bg, Batch{Writes: []Write{deltaWrite(id, 1, Operation{"a", []any{"s"}, "x"})}}); err == nil {
				t.Fatal("513 budget")
			}
			if !reflect.DeepEqual(before, snap(t, s)) {
				t.Fatal("budget changed state")
			}
			child := mint(t, s)
			apply(t, s, Write{Op: "create-conversation", Conversation: &Conversation{ID: child}})
			copyID := mint(t, s)
			target := d
			target.ID = copyID
			target.Owner = child
			target.Value = nil
			target.Version = 0
			before = snap(t, s)
			if _, err = s.Apply(bg, Batch{Writes: []Write{deltaWrite(id, 1, Operation{"t", []any{"s"}, 1}), {Op: "copy-document", Document: &target, Source: &DocumentCopySource{ID: id, At: CurrentDocumentPoint()}}}}); err == nil {
				t.Fatal("delta source copy fence")
			}
			if !reflect.DeepEqual(before, snap(t, s)) {
				t.Fatal("source fence changed state")
			}
		})
	}
}
func TestStorageDeltaSQLiteUncertainAndInvalidReplay(t *testing.T) {
	for _, mode := range []string{"before", "after"} {
		t.Run(mode, func(t *testing.T) {
			s, dir := newSQLiteTestStore(t)
			id := mint(t, s)
			d := Document{ID: id, Scope: "session", Kind: "delta.uncertain", Version: 1, Value: JSON{"s": "a"}}
			apply(t, s, putDocument(d))
			// Inject uncertainty before SQL or after a real atomic commit;
			// SQLite has no partial journal-frame write contract.
			if mode == "before" {
				s.appendFrame = func(byte, uint64, uint64, []byte) error { return errors.New("unknown append acknowledgement") }
			} else {
				installSQLiteAcknowledgement(s, func() error { return errors.New("lost commit acknowledgement") })
			}
			if _, err := s.Apply(bg, Batch{Writes: []Write{deltaWrite(id, 1, Operation{"a", []any{"s"}, "b"})}}); !errors.Is(err, ErrPoisoned) {
				t.Fatal(err)
			}
			if _, err := s.Snapshot(bg); !errors.Is(err, ErrPoisoned) {
				t.Fatal("poison exposed state", err)
			}
			if err := s.Close(bg); err != nil {
				t.Fatal(err)
			}
			reopened, err := openSQLiteTestDirectory(dir, SQLiteOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close(bg)
			got, _, err := reopened.DocumentAt(bg, id, CurrentDocumentPoint())
			if err != nil {
				t.Fatal(err)
			}
			want := "a"
			count := uint64(0)
			if mode == "after" {
				want = "ab"
				count = 1
			}
			if got.Value["s"] != want || got.DeltasSinceBase != count {
				t.Fatal("prefix adoption", got)
			}
		})
	}
	s, dir := newSQLiteTestStore(t)
	id := mint(t, s)
	d := Document{ID: id, Scope: "session", Kind: "delta.corrupt", Version: 1, Value: JSON{"s": "a"}}
	apply(t, s, putDocument(d))
	payload, err := encodeBounded(commitRecord{Seq: s.state.Seq + 1, Writes: []Write{deltaWrite(id, 2, Operation{"a", []any{"s"}, "b"})}}, s.limits, s.limits.MaxFramePayloadBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.exec("INSERT INTO frames(ordinal,kind,highwater,payload) VALUES(?,?,?,?)", int64(s.ordinal+1), 2, int64(s.state.HighWater), payload); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(bg); err != nil {
		t.Fatal(err)
	}
	if _, err = openSQLiteTestDirectory(dir, SQLiteOptions{}); !errors.Is(err, ErrCorrupt) {
		t.Fatal("complete cross-version delta frame accepted", err)
	}
}

func TestStorageDeltaRetainedAndRecordBudgets(t *testing.T) {
	for _, policy := range []string{"records", "retained"} {
		t.Run(policy, func(t *testing.T) {
			l := DefaultLimits()
			if policy == "records" {
				l.MaxRecords = 4
			} else {
				l.MaxRetainedBytes = 800
			}
			s, e := openTestSQLite(t, sqliteFixtureOptions{Limits: &l})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close(bg)
			id := mint(t, s)
			d := historyDocument(id, 1, "delta.limit", "", "rewindable", "asOf")
			d.Value = JSON{"s": ""}
			apply(t, s, putDocument(d))
			apply(t, s, deltaWrite(id, 1, Operation{"a", []any{"s"}, "x"}))
			before := snap(t, s)
			op := Operation{"a", []any{"s"}, "y"}
			if policy == "retained" {
				op = Operation{"a", []any{"s"}, string(bytes.Repeat([]byte{'y'}, 500))}
			}
			if _, e = s.Apply(bg, Batch{Writes: []Write{deltaWrite(id, 1, op)}}); e == nil {
				t.Fatal("retained/record budget accepted")
			}
			if !reflect.DeepEqual(before, snap(t, s)) {
				t.Fatal("budget rollback")
			}
		})
	}
}
