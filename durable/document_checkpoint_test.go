package durable

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func checkpointBackends(t *testing.T, fn func(*testing.T, *Session, func() *Session)) {
	t.Helper()
	for _, name := range []string{"memory", "journal"} {
		t.Run(name, func(t *testing.T) {
			image := &MemoryImage{}
			dir := filepath.Join(t.TempDir(), "private")
			open := func() *Session {
				var store Storage
				var err error
				if name == "memory" {
					store, err = OpenMemory(MemoryOptions{Image: image})
				} else {
					store, err = OpenJournal(dir, JournalOptions{})
				}
				if err != nil {
					t.Fatal(err)
				}
				session, err := OpenSession(store)
				if err != nil {
					t.Fatal(err)
				}
				return session
			}
			current := open()
			t.Cleanup(func() { current.Close(bg) })
			fn(t, current, func() *Session {
				if err := current.Close(bg); err != nil {
					t.Fatal(err)
				}
				current = open()
				return current
			})
		})
	}
}
func checkpointDef(t *testing.T, o DefinitionOptions) *DocumentDefinition {
	t.Helper()
	d, err := DefineDocument(o)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func checkpointCommit(t *testing.T, s *Session, fn func(*Tx) error) {
	t.Helper()
	if _, err := s.Commit(bg, fn); err != nil {
		t.Fatal(err)
	}
}
func checkpointSnapshot(t *testing.T, s *Session) Snapshot {
	t.Helper()
	v, err := s.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}
func TestDocumentCheckpointCountsSelectionUnloadAndReopen(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, reopen func() *Session) {
		seen := []uint64{}
		id := ID(0)
		def := checkpointDef(t, DefinitionOptions{Kind: "checkpoint.count", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(value JSON, ops []Operation, info CheckpointInfo) (bool, error) {
			seen = append(seen, info.DeltasSinceBase)
			if len(ops) == 0 {
				t.Fatal("empty predicate")
			}
			return info.DeltasSinceBase >= 2, nil
		}})
		checkpointCommit(t, s, func(tx *Tx) error {
			h, err := tx.AcquireDocument(def, 0, nil, nil)
			if err == nil {
				id = h.id
			}
			return err
		})
		change := func(n int) {
			checkpointCommit(t, s, func(tx *Tx) error {
				h, err := tx.AcquireDocument(def, 0, nil, nil)
				if err != nil {
					return err
				}
				return h.ApplyOperations([]Operation{{"s", []any{"n"}, n}})
			})
		}
		for n := 1; n <= 3; n++ {
			change(n)
		}
		if !reflect.DeepEqual(seen, []uint64{0, 1, 2}) || checkpointSnapshot(t, s).Documents[id].DeltasSinceBase != 0 {
			t.Fatal(seen)
		}
		if err := s.UnloadDocuments(bg); err != nil {
			t.Fatal(err)
		}
		change(4)
		s = reopen()
		change(5)
		if !reflect.DeepEqual(seen, []uint64{0, 1, 2, 0, 1}) || checkpointSnapshot(t, s).Documents[id].DeltasSinceBase != 2 {
			t.Fatal("cold counts", seen)
		}
		// A root replacement remains a delta unless the explicit predicate selects base.
		checkpointCommit(t, s, func(tx *Tx) error {
			h, err := tx.AcquireDocument(def, 0, nil, nil)
			if err != nil {
				return err
			}
			return h.Set(JSON{"n": 6})
		})
		if checkpointSnapshot(t, s).Documents[id].DeltasSinceBase != 0 {
			t.Fatal("checkpoint reset")
		}
	})
}
func TestDocumentCheckpointRequiredBasesAndStructuralIntent(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, reopen func() *Session) {
		calls := 0
		old := checkpointDef(t, DefinitionOptions{Kind: "checkpoint.intent", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"xs": []any{"a", "b"}}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { calls++; return false, nil }})
		checkpointCommit(t, s, func(tx *Tx) error {
			h, err := tx.AcquireDocument(old, 0, nil, nil)
			if err != nil {
				return err
			}
			return h.ApplyOperations([]Operation{{"p", []any{"xs"}, 2, 0, []any{"create"}}})
		})
		if calls != 0 {
			t.Fatal("create predicate")
		}
		before := checkpointSnapshot(t, s)
		checkpointCommit(t, s, func(tx *Tx) error {
			h, err := tx.AcquireDocument(old, 0, nil, nil)
			if err != nil {
				return err
			}
			if err = h.ApplyOperations(nil); err != nil {
				return err
			}
			return h.Update(func(v JSON) error { xs := v["xs"].([]any); v["xs"] = append(xs, "x")[:len(xs)]; return nil })
		})
		if calls != 0 || !reflect.DeepEqual(before, checkpointSnapshot(t, s)) {
			t.Fatal("empty candidate stored")
		}
		checkpointCommit(t, s, func(tx *Tx) error {
			h, err := tx.AcquireDocument(old, 0, nil, nil)
			if err != nil {
				return err
			}
			return h.ApplyOperations([]Operation{{"p", []any{"xs"}, 0, 1, []any{}}, {"p", []any{"xs"}, 0, 0, []any{"a"}}})
		})
		if calls != 1 || checkpointSnapshot(t, s).Seq != before.Seq+1 {
			t.Fatal("structural roundtrip lost")
		}
		checkpointCommit(t, s, func(tx *Tx) error {
			h, err := tx.AcquireDocument(old, 0, nil, nil)
			if err != nil {
				return err
			}
			return h.Set(JSON{"xs": []any{"replacement"}})
		})
		state := checkpointSnapshot(t, s)
		var id ID
		for found, d := range state.Documents {
			if d.Kind == "checkpoint.intent" {
				id = found
			}
		}
		if state.Documents[id].DeltasSinceBase != 2 || state.DocumentRevisions[id][2].Kind != "delta" || state.DocumentRevisions[id][2].Ops[0][0] != "r" {
			t.Fatal("rootreplacement delta/count", state.Documents[id])
		}
		migrationCalls := 0
		current := checkpointDef(t, DefinitionOptions{Kind: "checkpoint.intent", Version: 2, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{}, nil }, Migrate: func(v JSON, _ uint64) (JSON, error) { v["migrated"] = true; return v, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) {
			migrationCalls++
			return false, errors.New("required base must bypass")
		}})
		checkpointCommit(t, s, func(tx *Tx) error {
			h, err := tx.AcquireDocument(current, 0, nil, nil)
			if err != nil {
				return err
			}
			return h.ApplyOperations([]Operation{{"a", []any{"xs", 0}, "!"}})
		})
		state = checkpointSnapshot(t, s)
		if migrationCalls != 0 || state.Documents[id].Version != 2 || state.Documents[id].DeltasSinceBase != 0 {
			t.Fatal("migration requiredbase", state.Documents[id])
		}
		s = reopen()
		checkpointCommit(t, s, func(tx *Tx) error { return tx.RetireDefinition(current, 0, nil) })
		if migrationCalls != 0 {
			t.Fatal("retire without ordinary operations predicate")
		}
	})
}
func TestDocumentCheckpointFinalDetachedArgumentsAndWholeRollback(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, reopen func() *Session) {
		fail := true
		firstCalls := 0
		var escaped *DocumentHandle
		first := checkpointDef(t, DefinitionOptions{Kind: "checkpoint.first", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(v JSON, ops []Operation, info CheckpointInfo) (bool, error) {
			firstCalls++
			if v["n"].(json.Number) != "3" || len(ops) != 2 || info.DeltasSinceBase != 0 {
				t.Fatal("predicate not final", v, ops, info)
			}
			v["n"] = 99
			ops[0][1].([]any)[0] = "retained-mutation"
			return false, nil
		}})
		second := checkpointDef(t, DefinitionOptions{Kind: "checkpoint.second", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) {
			if fail {
				return false, errors.New("checkpoint failed")
			}
			return true, nil
		}})
		checkpointCommit(t, s, func(tx *Tx) error {
			if _, err := tx.AcquireDocument(first, 0, nil, nil); err != nil {
				return err
			}
			_, err := tx.AcquireDocument(second, 0, nil, nil)
			return err
		})
		before := checkpointSnapshot(t, s)
		mutation := func(tx *Tx) error {
			a, err := tx.AcquireDocument(first, 0, nil, nil)
			if err != nil {
				return err
			}
			escaped = a
			if err = a.ApplyOperations([]Operation{{"s", []any{"n"}, 1}}); err != nil {
				return err
			}
			if err = a.ApplyOperations([]Operation{{"s", []any{"n"}, 3}}); err != nil {
				return err
			}
			b, err := tx.AcquireDocument(second, 0, nil, nil)
			if err != nil {
				return err
			}
			return b.Set(JSON{"n": 2})
		}
		if _, err := s.Commit(bg, mutation); err == nil {
			t.Fatal("predicate failure accepted")
		}
		if firstCalls != 1 || !reflect.DeepEqual(before, checkpointSnapshot(t, s)) {
			t.Fatal("checkpoint rollback")
		}
		if err := escaped.ApplyOperations([]Operation{{"r", JSON{}}}); !errors.Is(err, ErrSealed) {
			t.Fatal("escaped handle", err)
		}
		fail = false
		checkpointCommit(t, s, mutation)
		value, ok, err := s.SnapshotDefinition(bg, first, 0, nil)
		if err != nil || !ok || value["n"].(json.Number) != "3" {
			t.Fatal("callback retained authority", value, err)
		}
		s = reopen()
		value, _, err = s.SnapshotDefinition(bg, first, 0, nil)
		if err != nil || value["n"].(json.Number) != "3" {
			t.Fatal("replay callback mutation", err)
		}
	})
}
func TestDocumentCheckpointOperationErrorHandledAndForkFence(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, reopen func() *Session) {
		def := checkpointDef(t, DefinitionOptions{Kind: "checkpoint.fork", Version: 1, Scope: "conversation", History: "rewindable", Fork: "current", Initial: func(JSON) (JSON, error) { return JSON{"s": "a"}, nil }})
		var entry ID
		checkpointCommit(t, s, func(tx *Tx) error {
			if _, err := tx.AcquireDocument(def, 1, nil, nil); err != nil {
				return err
			}
			id, err := tx.MintID()
			if err != nil {
				return err
			}
			entry = id
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "cutoff", Value: JSON{}})
		})
		checkpointCommit(t, s, func(tx *Tx) error {
			h, err := tx.AcquireDocument(def, 1, nil, nil)
			if err != nil {
				return err
			}
			if err = h.ApplyOperations([]Operation{{"a", []any{"s"}, "b"}, {"s", []any{"missing", "x"}, 1}}); err == nil {
				t.Fatal("bad operation accepted")
			}
			return h.ApplyOperations([]Operation{{"a", []any{"s"}, "c"}})
		})
		value, _, err := s.SnapshotDefinition(bg, def, 1, nil)
		if err != nil || value["s"] != "ac" {
			t.Fatal("partial failed method landed", value, err)
		}
		before := checkpointSnapshot(t, s)
		if _, err = s.Commit(bg, func(tx *Tx) error {
			if _, err := tx.ForkConversation(1, entry, 0); err != nil {
				return err
			}
			h, err := tx.AcquireDocument(def, 1, nil, nil)
			if err != nil {
				return err
			}
			return h.ApplyOperations([]Operation{{"a", []any{"s"}, "bad"}})
		}); err == nil {
			t.Fatal("current parent delta fence")
		}
		after := checkpointSnapshot(t, s)
		before.HighWater = after.HighWater
		if !reflect.DeepEqual(before, after) {
			t.Fatal("fork fence mutation")
		}
	})
}

func TestDocumentCheckpointMigrationResetCopyAndRetirement(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, reopen func() *Session) {
		calls := []uint64{}
		old := checkpointDef(t, DefinitionOptions{Kind: "checkpoint.version", Version: 1, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(_ JSON, _ []Operation, i CheckpointInfo) (bool, error) {
			calls = append(calls, i.DeltasSinceBase)
			return false, nil
		}})
		var cutoff ID
		checkpointCommit(t, s, func(tx *Tx) error {
			h, e := tx.AcquireDocument(old, 1, nil, nil)
			if e != nil {
				return e
			}
			if e = h.Set(JSON{"n": 1}); e != nil {
				return e
			}
			id, e := tx.MintID()
			if e != nil {
				return e
			}
			cutoff = id
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "cutoff", Value: JSON{}})
		})
		checkpointCommit(t, s, func(tx *Tx) error {
			h, e := tx.AcquireDocument(old, 1, nil, nil)
			if e != nil {
				return e
			}
			return h.Set(JSON{"n": 2})
		})
		current := checkpointDef(t, DefinitionOptions{Kind: "checkpoint.version", Version: 2, Scope: "conversation", History: "rewindable", Fork: "asOf", Initial: func(JSON) (JSON, error) { return JSON{}, nil }, Migrate: func(v JSON, _ uint64) (JSON, error) { return v, nil }, CheckpointWhen: func(_ JSON, _ []Operation, i CheckpointInfo) (bool, error) {
			calls = append(calls, i.DeltasSinceBase)
			return true, nil
		}})
		checkpointCommit(t, s, func(tx *Tx) error { _, e := tx.AcquireDocument(current, 1, nil, nil); return e })
		checkpointCommit(t, s, func(tx *Tx) error {
			h, e := tx.AcquireDocument(current, 1, nil, nil)
			if e != nil {
				return e
			}
			if e = h.Set(JSON{"n": 3}); e != nil {
				return e
			}
			return h.Retire()
		})
		if !reflect.DeepEqual(calls, []uint64{0, 0}) {
			t.Fatal("requiredbase reset/retirement predicate", calls)
		}
		var child Conversation
		checkpointCommit(t, s, func(tx *Tx) error { var e error; child, e = tx.ForkConversation(1, cutoff, 0); return e })
		// Definition-free copy preserved source version1 at cutoff and count0.
		state := checkpointSnapshot(t, s)
		for _, d := range state.Documents {
			if d.Owner == child.ID && (d.Version != 1 || d.DeltasSinceBase != 0) {
				t.Fatal("copy version/base", d)
			}
		}
		checkpointCommit(t, s, func(tx *Tx) error {
			h, e := tx.AcquireDocument(current, child.ID, nil, nil)
			if e != nil {
				return e
			}
			return h.Set(JSON{"n": 4})
		})
		if len(calls) != 2 {
			t.Fatal("copy/migration called predicate")
		}
		s = reopen()
		value, ok, e := s.SnapshotDefinitionAsOf(bg, current, child.ID, nil, cutoff)
		if e != nil || !ok || value["n"].(json.Number) != "1" {
			t.Fatal("ancestor delta/migration", value, e)
		}
	})
}

func TestDocumentCheckpointInvalidFinalReferencesSkipPredicate(t *testing.T) {
	checkpointBackends(t, func(t *testing.T, s *Session, _ func() *Session) {
		calls := 0
		def := checkpointDef(t, DefinitionOptions{Kind: "checkpoint.validation", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"n": 0}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) { calls++; return false, nil }})
		checkpointCommit(t, s, func(tx *Tx) error { _, e := tx.AcquireDocument(def, 0, nil, nil); return e })
		before := checkpointSnapshot(t, s)
		_, err := s.Commit(bg, func(tx *Tx) error {
			h, e := tx.AcquireDocument(def, 0, nil, nil)
			if e != nil {
				return e
			}
			if e = h.Set(JSON{"n": 1}); e != nil {
				return e
			}
			id, e := tx.MintID()
			if e != nil {
				return e
			}
			return tx.AppendEntry(Entry{ID: id, Conversation: ID(MaxID), Kind: "invalid", Value: JSON{}})
		})
		if err == nil || calls != 0 {
			t.Fatal("predicate ran before invalid final references", calls, err)
		}
		after := checkpointSnapshot(t, s)
		before.HighWater = after.HighWater
		if !reflect.DeepEqual(before, after) {
			t.Fatal("invalid reference rollback")
		}
	})
}

func TestDocumentCheckpointSealedPredicateAndTypedExactBudget(t *testing.T) {
	l := DefaultLimits()
	l.MaxDocumentBytes = 512
	l.MaxRecordBytes = 2048
	l.MaxStringBytes = 1024
	s0, e := OpenMemory(MemoryOptions{Limits: &l})
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenSession(s0)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(bg)
	var held *DocumentHandle
	calls := 0
	def := checkpointDef(t, DefinitionOptions{Kind: "checkpoint.bytes", Version: 1, Scope: "session", Initial: func(JSON) (JSON, error) { return JSON{"s": ""}, nil }, CheckpointWhen: func(JSON, []Operation, CheckpointInfo) (bool, error) {
		calls++
		if _, e := held.Get(); !errors.Is(e, ErrSealed) {
			t.Fatal("predicate reopened handle", e)
		}
		return false, nil
	}})
	checkpointCommit(t, s, func(tx *Tx) error { _, e := tx.AcquireDocument(def, 0, nil, nil); return e })
	checkpointCommit(t, s, func(tx *Tx) error {
		h, e := tx.AcquireDocument(def, 0, nil, nil)
		if e != nil {
			return e
		}
		held = h
		return h.ApplyOperations([]Operation{{"a", []any{"s"}, string(bytes.Repeat([]byte{'x'}, 504))}})
	})
	before := checkpointSnapshot(t, s)
	if _, e = s.Commit(bg, func(tx *Tx) error {
		h, e := tx.AcquireDocument(def, 0, nil, nil)
		if e != nil {
			return e
		}
		held = h
		return h.ApplyOperations([]Operation{{"a", []any{"s"}, "x"}})
	}); e == nil {
		t.Fatal("typed513 accepted")
	}
	if calls != 1 || !reflect.DeepEqual(before, checkpointSnapshot(t, s)) {
		t.Fatal("typed budget rollback")
	}
}
