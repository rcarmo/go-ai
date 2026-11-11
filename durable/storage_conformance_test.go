package durable

import (
	"bytes"
	"context"

	"encoding/json"
	"errors"
	"fmt"

	"math"
	"os"

	"path/filepath"
	"reflect"
	"strings"

	"testing"
)

var bg = context.Background()

type sqliteFixture struct {
	store  Storage
	reopen func() Storage
}

type sqliteFixtureFile struct{ path string }
type sqliteFixtureOptions struct {
	Limits *Limits
	File   *sqliteFixtureFile
}

func openTestSQLite(t testing.TB, options sqliteFixtureOptions) (*SQLiteStorage, error) {
	t.Helper()
	path := ""
	if options.File != nil {
		path = options.File.path
	}
	if path == "" {
		path = filepath.Join(t.TempDir(), "store.sqlite")
		if options.File != nil {
			options.File.path = path
		}
	}
	return OpenSQLite(path, SQLiteOptions{Limits: options.Limits})
}
func newTestSQLite(t testing.TB) (*SQLiteStorage, error) {
	t.Helper()
	return openTestSQLite(t, sqliteFixtureOptions{})
}
func openSQLiteTestDirectory(dir string, options SQLiteOptions) (*SQLiteStorage, error) {
	return OpenSQLite(filepath.Join(dir, "store.sqlite"), options)
}

// sqliteStore provides a store whose reserved root conversation already exists.
// Native Storage no longer fabricates conversation 1, so the fixture creates it
// as its first committed write, the way the original tests acquire the root.
func sqliteStore(t *testing.T, fn func(*testing.T, sqliteFixture), configured ...Limits) {
	t.Helper()
	sqliteStoreFresh(t, func(t *testing.T, b sqliteFixture) {
		seedRootConversation(t, b.store)
		fn(t, b)
	}, configured...)
}

// sqliteStoreFresh provides a store in its untouched initial state.
func sqliteStoreFresh(t *testing.T, fn func(*testing.T, sqliteFixture), configured ...Limits) {
	t.Helper()
	if len(configured) > 1 {
		t.Fatal("multiple SQLite limit profiles")
	}
	var limits *Limits
	if len(configured) == 1 {
		limits = &configured[0]
	}
	t.Run("sqlite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "store.sqlite")
		open := func() *SQLiteStorage {
			s, e := OpenSQLite(path, SQLiteOptions{Limits: limits})
			if e != nil {
				t.Fatal(e)
			}
			return s
		}
		store := open()
		closeStore := func() {
			select {
			case <-store.closeDone:
				if store.closeErr != nil {
					t.Fatal(store.closeErr)
				}
			default:
				if e := store.Close(bg); e != nil {
					t.Fatal(e)
				}
			}
		}
		t.Cleanup(closeStore)
		fn(t, sqliteFixture{store: store, reopen: func() Storage { closeStore(); store = open(); return store }})
	})
}

// seedRootConversation mirrors the original tests acquiring the reserved root
// through Harness.Root()/tx.createRootConversation(). Native Storage no longer
// fabricates it, so fixtures that address conversation 1 must create it first.
func seedRootConversation(t testing.TB, s Storage) {
	t.Helper()
	state, err := s.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := state.Conversations[1]; ok {
		return
	}
	if _, err := s.Apply(bg, Batch{[]Write{{Op: "create-conversation", Conversation: &Conversation{ID: 1}}}}); err != nil {
		t.Fatal(err)
	}
}

func mint(t *testing.T, s Storage) ID {
	t.Helper()
	id, e := s.MintID(bg)
	if e != nil {
		t.Fatal(e)
	}
	return id
}
func apply(t *testing.T, s Storage, ws ...Write) uint64 {
	t.Helper()
	// Fixtures that address the reserved root create it explicitly, matching the
	// original tests acquiring conversation 1 through tx.createRootConversation().
	creates, references := false, false
	for _, w := range ws {
		if w.Conversation != nil && w.Conversation.ID == 1 {
			creates = true
		}
		if (w.Entry != nil && w.Entry.Conversation == 1) ||
			(w.Task != nil && w.Task.Conversation == 1) ||
			(w.Submission != nil && w.Submission.Conversation == 1) ||
			(w.Document != nil && w.Document.Scope == "conversation" && w.Document.Owner == 1) {
			references = true
		}
	}
	if references && !creates {
		seedRootConversation(t, s)
	}
	seq, e := s.Apply(bg, Batch{ws})
	if e != nil {
		t.Fatal(e)
	}
	return seq
}
func snap(t *testing.T, s Storage) Snapshot {
	t.Helper()
	v, e := s.Snapshot(bg)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func entry(id ID, value JSON) Write {
	return Write{Op: "append-entry", Entry: &Entry{ID: id, Conversation: 1, Kind: "message", Value: value}}
}
func rejected(t *testing.T, e error) {
	t.Helper()
	var r *StorageRejected
	if !errors.As(e, &r) {
		t.Fatalf("want StorageRejected, got %v", e)
	}
}

func TestStorageConformance(t *testing.T) {
	sqliteStoreFresh(t, runStorageConformance)
}
func runStorageConformance(t *testing.T, b sqliteFixture) {
	s := b.store
	t.Run("01_reserved_root_absent_then_immutable", func(t *testing.T) {
		v := snap(t, s)
		if v.HighWater != 1 || len(v.Conversations) != 0 {
			t.Fatal(v)
		}
		apply(t, s, Write{Op: "create-conversation", Conversation: &Conversation{ID: 1}})
		_, e := s.Apply(bg, Batch{[]Write{{Op: "create-conversation", Conversation: &Conversation{ID: 1}}}})
		rejected(t, e)
	})
	// 02/03/04: one mixed atomic revision, detached originals and returned graphs.
	conv, task, sub, doc := mint(t, s), mint(t, s), mint(t, s), mint(t, s)
	e1, e2 := mint(t, s), mint(t, s)
	value := JSON{"__proto__": JSON{"constructor": "data"}, "\x00 key ": []any{"original", json.Number("12345678901234567890")}}
	seq := apply(t, s, Write{Op: "create-conversation", Conversation: &Conversation{ID: conv, Owner: task}}, Write{Op: "put-task", Task: &Task{ID: task, Conversation: conv, Kind: "generation", Status: "waiting", Checkpoint: JSON{"step": 1}}}, Write{Op: "put-submission", Submission: &Submission{ID: sub, Conversation: conv, RequestID: " \x00exact ", Type: "write", Status: "pending", Value: JSON{}}}, Write{Op: "put-document", Document: &Document{ID: doc, Scope: "conversation", Owner: conv, Kind: "app.state", Key: "\x00 exact ", Version: 1, Value: value}}, entry(e2, JSON{"secondID": "first"}), entry(e1, JSON{"firstID": "second"}))
	value["\x00 key "].([]any)[0] = "mutated"
	v := snap(t, s)
	if seq != 2 || v.Seq != 2 || v.Documents[doc].Value["\x00 key "].([]any)[0] != "original" {
		t.Fatal("02/03/04 mixed/detached state")
	}
	v.Documents[doc].Value["__proto__"].(map[string]any)["constructor"] = "changed"
	if snap(t, s).Documents[doc].Value["__proto__"].(map[string]any)["constructor"] != "data" {
		t.Fatal("03 detached reads")
	}
	t.Run("02_20_partial_supported_batch_rollback", func(t *testing.T) {
		id := mint(t, s)
		_, e := s.Apply(bg, Batch{[]Write{entry(id, JSON{}), {Op: "put-document", Document: &Document{ID: doc, Scope: "session", Kind: "app.state", Version: 1, Value: JSON{}}}}})
		rejected(t, e)
		if len(snap(t, s).Entries) != 2 || snap(t, s).Seq != 2 {
			t.Fatal("partial batch visibility")
		}
	})
	t.Run("05_06_entry_order_and_stable_cursor", func(t *testing.T) {
		r, e := s.Entries(bg, 1, EntryCursor{}, 1)
		if e != nil || len(r) != 1 || r[0].ID != e2 {
			t.Fatalf("%v %v", r, e)
		}
		cursor := EntryCursor{1, r[0].Seq, r[0].Position}
		apply(t, s, entry(mint(t, s), JSON{}))
		r, e = s.Entries(bg, 1, cursor, 10)
		if e != nil || len(r) != 2 || r[0].ID != e1 {
			t.Fatal("entry cursor changed")
		}
		_, e = s.Entries(bg, conv, cursor, 1)
		rejected(t, e)
	})
	t.Run("07_08_partial_owner_conjunctive", func(t *testing.T) {
		r, e := s.Conversations(bg, Query{Owner: task, Conversation: conv, Limit: 1})
		if e != nil || len(r) != 1 || r[0].ID != conv {
			t.Fatal(r, e)
		}
		r, e = s.Conversations(bg, Query{After: 1, Limit: 1})
		if e != nil || len(r) != 1 || r[0].ID != conv {
			t.Fatal(r, e)
		}
	})
	t.Run("10_11_full_replacement_status_raw_no_scheduler", func(t *testing.T) {
		apply(t, s, Write{Op: "put-task", Task: &Task{ID: task, Conversation: conv, Kind: "generation", Status: "completing", Checkpoint: JSON{"new": true}}})
		state, e := s.Snapshot(bg)
		r := state.Tasks[task]
		if e != nil || r.Conversation != conv || r.Status != "completing" || r.Kind != "generation" || len(r.Checkpoint) != 1 {
			t.Fatal(r, e)
		}
		// Legacy private envelopes have no invented source input/version.
		if _, _, err := s.Task(bg, task); err == nil {
			t.Fatal("legacy task gained canonical data")
		}
	})
	t.Run("12_13_request_identity_write_union", func(t *testing.T) {
		r, ok, e := s.Request(bg, conv, " \x00exact ")
		if e != nil || !ok || r.ID != sub || r.Type != "write" {
			t.Fatal(r, ok, e)
		}
		apply(t, s, Write{Op: "put-submission", Submission: &Submission{ID: sub, Conversation: conv, RequestID: r.RequestID, Type: "write", Status: "done", Value: JSON{"result": true}}})
		id := mint(t, s)
		_, e = s.Apply(bg, Batch{[]Write{{Op: "put-submission", Submission: &Submission{ID: id, Conversation: conv, RequestID: r.RequestID, Type: "follow-up", Status: "pending", Value: JSON{}}}}})
		rejected(t, e)
		id = mint(t, s)
		apply(t, s, Write{Op: "put-submission", Submission: &Submission{ID: id, Conversation: 1, RequestID: r.RequestID, Type: "follow-up", Status: "pending", Value: JSON{}}})
		_, e = s.Apply(bg, Batch{[]Write{{Op: "put-submission", Submission: &Submission{ID: sub, Conversation: conv, RequestID: r.RequestID, Type: "follow-up", Status: "pending", Value: JSON{}}}}})
		rejected(t, e)
	})
	t.Run("14_17_18_19_21_partial_current_only", func(t *testing.T) {
		old := snap(t, s).Documents[doc]
		old.Version = 2
		old.Value = JSON{"base": true}
		apply(t, s, Write{Op: "put-document", Document: &old})
		key := old.Key
		r, e := s.Documents(bg, Query{Scope: "conversation", Owner: conv, Kind: "app.state", Key: &key, Limit: 1})
		if e != nil || len(r) != 1 || r[0].Version != 2 {
			t.Fatal(r, e)
		}
		apply(t, s, Write{Op: "retire-document", Document: &old})
		id := mint(t, s)
		old.ID = id
		old.CreatedAt, old.RetiredAt = 0, 0 // new incarnation stamps belong to storage
		old.Version = 1
		apply(t, s, Write{Op: "put-document", Document: &old})
		r, e = s.Documents(bg, Query{Scope: "conversation", Owner: conv, Key: &key, Limit: 2})
		if e != nil || len(r) != 1 || r[0].ID != id {
			t.Fatal(r, e)
		}
		old.ID = mint(t, s)
		old.Kind = " bad "
		_, e = s.Apply(bg, Batch{[]Write{{Op: "put-document", Document: &old}}})
		rejected(t, e)
	})
	t.Run("09_15_16_UNSUPPORTED_rejection_not_parity", func(t *testing.T) {
		for _, op := range []string{"fork", "delta", "copy", "historical"} {
			_, e := s.Apply(bg, Batch{[]Write{{Op: op, Document: &Document{ID: doc}}}})
			if !errors.Is(e, ErrUnsupported) {
				t.Fatalf("unsupported %s %v", op, e)
			}
		}
		id := mint(t, s)
		_, e := s.Apply(bg, Batch{[]Write{{Op: "create-conversation", Conversation: &Conversation{ID: id, Parent: 1}}}})
		if !errors.Is(e, ErrUnsupported) {
			t.Fatal(e)
		}
	})
	before := snap(t, s)
	s = b.reopen()
	after := snap(t, s)
	if !equalSnapshot(before, after) {
		t.Fatal("reopen differs")
	}
	t.Run("22_global_namespace", func(t *testing.T) {
		id := mint(t, s)
		apply(t, s, entry(id, JSON{}))
		_, e := s.Apply(bg, Batch{[]Write{{Op: "put-task", Task: &Task{ID: id, Conversation: 1, Kind: "task", Status: "pending", Checkpoint: JSON{}}}}})
		rejected(t, e)
		_, e = s.Apply(bg, Batch{[]Write{entry(ID(MaxID+1), JSON{})}})
		rejected(t, e)
	})
	t.Run("23_every_operation_after_close", func(t *testing.T) {
		if e := s.Close(bg); e != nil {
			t.Fatal(e)
		}
		checks := []func() error{func() error { _, e := s.MintID(bg); return e }, func() error { _, e := s.Apply(bg, Batch{}); return e }, func() error { _, e := s.Snapshot(bg); return e }, func() error { _, e := s.Limits(); return e }, func() error { _, e := s.Conversations(bg, Query{Limit: 1}); return e }, func() error { _, e := s.Entries(bg, 1, EntryCursor{}, 1); return e }, func() error { _, _, e := s.Task(bg, 1); return e }, func() error { _, e := s.Submissions(bg, Query{Limit: 1}); return e }, func() error { _, e := s.Documents(bg, Query{Limit: 1}); return e }, func() error { _, _, e := s.Request(bg, 1, "id"); return e }}
		for _, f := range checks {
			if e := f(); !errors.Is(e, ErrClosed) {
				t.Fatal(e)
			}
		}
	})
}
func equalSnapshot(a, b Snapshot) bool {
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(aa, bb)
}

func TestStrictJSONAndBudgets(t *testing.T) {
	l := DefaultLimits()
	for _, data := range []string{`{"x":1,"x":2}`, `{"a":{"x":1,"x":2}}`, `{"a":"\ud800"}`, `{"a":"\udc00"}`, `{"a":NaN}`, `{"a":01}`, `{"a":1.}`, `{"a":1e}`, `{"a":true} {}`, `{"a":"` + string([]byte{0xff}) + `"}`, `[]`} {
		t.Run(fmt.Sprintf("bad%q", data), func(t *testing.T) { var v JSON; rejected(t, decodeStrict([]byte(data), l, l.MaxDocumentBytes, &v)) })
	}
	for _, v := range []JSON{{"x": math.NaN()}, {"x": math.Inf(1)}, {"x": func() {}}, {"x": json.Number("01")}, {"x": string([]byte{0xff})}} {
		_, e := copyObject(v, l)
		rejected(t, e)
	}
	cycle := JSON{}
	cycle["cycle"] = cycle
	_, e := copyObject(cycle, l)
	rejected(t, e)
	v := JSON{"\x00__proto__ ": json.Number("900719925474099123456789"), "pair": "𝄞"}
	owned, e := copyObject(v, l)
	if e != nil || owned["\x00__proto__ "] != json.Number("900719925474099123456789") {
		t.Fatal(owned, e)
	}
	small := l
	small.MaxStringBytes = 8
	_, e = copyObject(JSON{"x": json.Number(strings.Repeat("1", 100000))}, small)
	rejected(t, e)
	small = l
	small.MaxDepth = 2
	_, e = copyObject(JSON{"x": JSON{"y": JSON{}}}, small)
	rejected(t, e)
	small = l
	small.MaxMembers = 1
	_, e = copyObject(JSON{"a": 1, "b": 2}, small)
	rejected(t, e)
	small = l
	small.MaxNodes = 2
	_, e = copyObject(JSON{"a": 1}, small)
	rejected(t, e)
	_, e = encodeBounded(JSON{"x": strings.Repeat("\x00", 100)}, l, 150)
	rejected(t, e)
	var c Conversation
	for _, token := range []string{"9007199254740992", "1e2", "1.5", "18446744073709551616"} {
		e = decodeStrict([]byte(`{"id":`+token+`}`), l, 1000, &c)
		if token == "9007199254740992" {
			if e != nil {
				t.Fatal(e)
			}
			s, _ := newTestSQLite(t)
			seedRootConversation(t, s)
			_, e = s.Apply(bg, Batch{[]Write{{Op: "create-conversation", Conversation: &c}}})
			_ = s.Close(bg)
		}
		rejected(t, e)
	}
}

func TestLimitsPreAdmissionSQLite(t *testing.T) {
	for _, name := range []string{"MaxDocumentBytes", "MaxRecordBytes", "MaxFramePayloadBytes", "MaxStringBytes", "MaxRequestIDBytes", "MaxDepth", "MaxMembers", "MaxNodes", "MaxWrites", "MaxPage", "MaxRecords", "MaxRetainedBytes"} {
		t.Run(name, func(t *testing.T) {
			for _, kind := range []string{"sqlite"} {
				t.Run(kind, func(t *testing.T) {
					l := DefaultLimits()
					switch name {
					case "MaxDocumentBytes":
						l.MaxDocumentBytes = 20
					case "MaxRecordBytes":
						l.MaxDocumentBytes = 20
						l.MaxRecordBytes = 128
						l.MaxStringBytes = 64
						l.MaxRequestIDBytes = 8
					case "MaxFramePayloadBytes":
						l.MaxFramePayloadBytes = 512
						l.MaxRecordBytes = 256
						l.MaxDocumentBytes = 128
						l.MaxStringBytes = 128
						l.MaxRequestIDBytes = 32
					case "MaxStringBytes":
						l.MaxStringBytes = 16
						l.MaxRequestIDBytes = 8
					case "MaxRequestIDBytes":
						l.MaxRequestIDBytes = 2
					case "MaxDepth":
						l.MaxDepth = 4
					case "MaxMembers":
						l.MaxMembers = 16
					case "MaxNodes":
						l.MaxNodes = 64
					case "MaxWrites":
						l.MaxWrites = 1
					case "MaxPage":
						l.MaxPage = 1
					case "MaxRecords":
						l.MaxRecords = 1
					case "MaxRetainedBytes":
						l.MaxRetainedBytes = 100
					}
					var s Storage
					var e error
					var j *SQLiteStorage

					j, e = openSQLiteTestDirectory(filepath.Join(t.TempDir(), "private"), SQLiteOptions{Limits: &l})
					s = j

					if e != nil {
						t.Fatal(e)
					}
					defer s.Close(bg)
					// The reserved root is created only when the profile can encode
					// its own write; MaxStringBytes is below the operation name length.
					if name != "MaxStringBytes" {
						seedRootConversation(t, s)
					}
					id := mint(t, s)
					ws := []Write{entry(id, JSON{})}
					switch name {
					case "MaxDocumentBytes":
						ws = []Write{{Op: "put-document", Document: &Document{ID: id, Scope: "session", Kind: "state", Version: 1, Value: JSON{"v": strings.Repeat("x", 30)}}}}
					case "MaxRecordBytes":
						ws = []Write{entry(id, JSON{"v": strings.Repeat("x", 64)})}
					case "MaxFramePayloadBytes":
						for k := 0; k < 3; k++ {
							ws = append(ws, entry(mint(t, s), JSON{"v": strings.Repeat("x", 128)}))
						}
					case "MaxStringBytes":
						// A session document keeps the check on the string budget itself:
						// this profile cannot even encode a conversation-creating write.
						ws = []Write{{Op: "put-document", Document: &Document{ID: id, Scope: "session", Kind: "state", Version: 1, Value: JSON{"v": strings.Repeat("x", 17)}}}}
					case "MaxRequestIDBytes":
						ws = []Write{{Op: "put-submission", Submission: &Submission{ID: id, Conversation: 1, RequestID: "abc", Type: "write", Status: "pending", Value: JSON{}}}}
					case "MaxDepth":
						ws = []Write{entry(id, JSON{"nested": JSON{"x": JSON{}}})}
					case "MaxMembers":
						ws = []Write{entry(id, JSON{"a": make([]any, 17)})}
					case "MaxNodes":
						ws = []Write{entry(id, JSON{"a": make([]any, 100)})}
					case "MaxWrites":
						ws = append(ws, entry(mint(t, s), JSON{}))
					case "MaxPage":
						_, e = s.Entries(bg, 1, EntryCursor{}, 2)
						rejected(t, e)
						return
					case "MaxRetainedBytes":
						ws = []Write{entry(id, JSON{"v": strings.Repeat("x", 128)})}
					}
					before := snap(t, s)
					_, e = s.Apply(bg, Batch{ws})
					rejected(t, e)
					if name == "MaxStringBytes" && !strings.Contains(e.Error(), "string") {
						t.Fatalf("string budget not exercised: %v", e)
					}
					after := snap(t, s)
					if !equalSnapshot(before, after) {
						t.Fatal("rejected limit changed state")
					}
					if j != nil && j.poisoned {
						t.Fatal("limit admitted or poisoned")
					}
				})
			}
		})
	}
}

func TestBootstrapIndependentDataBudgets(t *testing.T) {
	l := DefaultLimits()
	l.MaxMembers = 1
	l.MaxNodes = 2
	l.MaxDepth = 1
	dir := filepath.Join(t.TempDir(), "private")
	s, e := openSQLiteTestDirectory(dir, SQLiteOptions{Limits: &l})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Close(bg); e != nil {
		t.Fatal(e)
	}
	s, e = openSQLiteTestDirectory(dir, SQLiteOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(bg)
	// A valid profile may deliberately prohibit any data envelope larger than
	// its budget. Config remains governed by fixed16KiB bootstrap budget.
	_, e = s.MintID(bg)
	rejected(t, e)
	if s.poisoned {
		t.Fatal("deterministic budget poisoned")
	}
	invalid := DefaultLimits()
	invalid.MaxJournalBytes = 1
	notCreated := filepath.Join(t.TempDir(), "must-not-create")
	_, e = openSQLiteTestDirectory(notCreated, SQLiteOptions{Limits: &invalid})
	rejected(t, e)
	if _, e = os.Stat(notCreated); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("invalid profile created directory")
	}
}

func TestSequenceAndOrdinalExhaustionBeforeIO(t *testing.T) {
	for _, kind := range []string{"seq", "ordinal"} {
		t.Run(kind, func(t *testing.T) {
			s, dir := newSQLiteTestStore(t)
			seedRootConversation(t, s)
			_ = dir
			id := mint(t, s)
			if kind == "seq" {
				s.state.Seq = MaxID - 1
			} else {
				s.ordinal = MaxID - 1
			}
			if _, e := s.Apply(bg, Batch{[]Write{entry(id, JSON{})}}); e != nil {
				t.Fatal(e)
			}
			before := sqliteFrameCount(s)
			_, e := s.Apply(bg, Batch{[]Write{entry(id, JSON{})}})
			rejected(t, e)
			if sqliteFrameCount(s) != before || s.poisoned {
				t.Fatal("exhaustion admitted")
			}
		})
	}
}

func TestPublicLowLevelJSONAndEnvelopeBounds(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s := b.store
		id := mint(t, s)
		marshalCalls.Store(0)
		for _, value := range []any{struct{ Value string }{"no implicit struct"}, hostileString("noMarshal"), json.Number(strings.Repeat("1", 1<<20))} {
			_, e := s.Apply(bg, Batch{[]Write{entry(id, JSON{"v": value})}})
			rejected(t, e)
		}
		if marshalCalls.Load() != 0 || snap(t, s).Seq != 1 {
			t.Fatal("marshal/visibility")
		}
	})
	// Actual escaped envelope overhead can exceed frame despite small raw input.
	l := DefaultLimits()
	l.MaxFramePayloadBytes = 1024
	l.MaxRecordBytes = 512
	l.MaxDocumentBytes = 256
	l.MaxStringBytes = 128
	l.MaxRequestIDBytes = 16
	s, e := openTestSQLite(t, sqliteFixtureOptions{Limits: &l})
	if e != nil {
		t.Fatal(e)
	}
	seedRootConversation(t, s)
	defer s.Close(bg)
	var ws []Write
	for k := 0; k < 5; k++ {
		ws = append(ws, entry(mint(t, s), JSON{"nul": strings.Repeat("\x00", 30)}))
	}
	_, e = s.Apply(bg, Batch{ws})
	rejected(t, e)
	if snap(t, s).Seq != 1 {
		t.Fatal("frame split/partialadoption")
	}
}

func TestPrivateCandidateCOWPreservesPublicDetachAndRollback(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		s := b.store
		doc, task, sub, ent := mint(t, s), mint(t, s), mint(t, s), mint(t, s)
		value := JSON{"nested": JSON{"v": "base"}, "items": []any{JSON{"v": "base"}}}
		apply(t, s, entry(ent, value), Write{Op: "put-task", Task: &Task{ID: task, Conversation: 1, Kind: "test", Status: "pending", Checkpoint: value}}, Write{Op: "put-submission", Submission: &Submission{ID: sub, Conversation: 1, Type: "write", Status: "done", Value: value}}, Write{Op: "put-document", Document: &Document{ID: doc, Scope: "conversation", Owner: 1, Kind: "app.cow", Version: 1, Value: value}})
		before := snap(t, s)
		h := sessionFor(t, s)
		var escaped *DocumentHandle
		var stagedInput, candidate JSON
		sentinel := errors.New("rollback candidate")
		_, e := h.Commit(bg, func(tx *Tx) error {
			var err error
			escaped, err = tx.Document(doc)
			if err != nil {
				return err
			}
			// This invokes currentDocument on a private COW candidate repeatedly.
			stagedInput = JSON{"nested": JSON{"v": "set"}, "items": []any{JSON{"v": "set"}}}
			if err = escaped.Set(stagedInput); err != nil {
				return err
			}
			stagedInput["nested"].(JSON)["v"] = "caller mutated afterSet"
			if err = escaped.Update(func(v JSON) error { candidate = v; v["nested"].(map[string]any)["v"] = "updated"; return nil }); err != nil {
				return err
			}
			candidate["nested"].(map[string]any)["v"] = "caller mutated afterUpdate"
			got, err := escaped.Get()
			if err != nil {
				return err
			}
			if got["nested"].(map[string]any)["v"] != "updated" {
				t.Fatal("staged copy boundary lost")
			}
			got["items"].([]any)[0].(map[string]any)["v"] = "returned Get mutated"
			if err = escaped.Update(func(v JSON) error { v["nested"].(map[string]any)["v"] = "failed update"; return sentinel }); !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
			got, err = escaped.Get()
			if err != nil {
				return err
			}
			if got["nested"].(map[string]any)["v"] != "updated" || got["items"].([]any)[0].(map[string]any)["v"] != "set" {
				t.Fatal("candidate alias/failedUpdate leaked")
			}
			if err = escaped.Set(JSON{"invalid": math.NaN()}); err == nil {
				t.Fatal("invalid Set accepted")
			}
			return sentinel
		})
		if !errors.Is(e, sentinel) {
			t.Fatal(e)
		}
		after, e := h.Snapshot(bg)
		if e != nil || !equalSnapshot(before, after) {
			t.Fatal("rollback mutated immutable base", e)
		}
		if e = escaped.Set(JSON{}); !errors.Is(e, ErrSealed) {
			t.Fatal("escaped handle writable", e)
		}
		_, e = h.Commit(bg, func(tx *Tx) error {
			d, e := tx.Document(doc)
			if e != nil {
				return e
			}
			return d.Update(func(v JSON) error { v["nested"].(map[string]any)["v"] = "committed"; return nil })
		})
		if e != nil {
			t.Fatal(e)
		}
		latest, e := h.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		if before.Documents[doc].Value["nested"].(map[string]any)["v"] != "base" || latest.Documents[doc].Value["nested"].(map[string]any)["v"] != "committed" {
			t.Fatal("prior snapshot mutated")
		}
		// Every public table/query returns detached nested ownership, including rows
		// untouched by the document replacement and shared internally by preparation.
		latest.Entries[ent].Value["nested"].(map[string]any)["v"] = "read mutate"
		latest.Tasks[task].Checkpoint["nested"].(map[string]any)["v"] = "read mutate"
		latest.Submissions[sub].Value["nested"].(map[string]any)["v"] = "read mutate"
		latest.Documents[doc].Value["items"].([]any)[0].(map[string]any)["v"] = "read mutate"
		es, e := s.Entries(bg, 1, EntryCursor{}, 1)
		if e != nil {
			t.Fatal(e)
		}
		es[0].Value["nested"].(map[string]any)["v"] = "query mutate"
		ts, e := s.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		ts.Tasks[task].Checkpoint["nested"].(map[string]any)["v"] = "query mutate"
		ss, e := s.Submissions(bg, Query{Limit: 1})
		if e != nil {
			t.Fatal(e)
		}
		ss[0].Value["nested"].(map[string]any)["v"] = "query mutate"
		ds, e := s.Documents(bg, Query{Limit: 1})
		if e != nil {
			t.Fatal(e)
		}
		ds[0].Value["nested"].(map[string]any)["v"] = "query mutate"
		authoritative, e := h.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		if authoritative.Entries[ent].Value["nested"].(map[string]any)["v"] != "base" || authoritative.Tasks[task].Checkpoint["nested"].(map[string]any)["v"] != "base" || authoritative.Submissions[sub].Value["nested"].(map[string]any)["v"] != "base" || authoritative.Documents[doc].Value["nested"].(map[string]any)["v"] != "committed" {
			t.Fatal("public internal sharing")
		}
		if e = h.Close(bg); e != nil {
			t.Fatal(e)
		}
		reopened := reopenStoreAfterHarnessClose(t, s)
		defer reopened.Close(bg)
		if !equalSnapshot(authoritative, snap(t, reopened)) {
			t.Fatal("SQLite reopen changed committed ownership")
		}
	})
}

func TestPrivateCandidateTablesNeverModifyBaseTables(t *testing.T) {
	l := DefaultLimits()
	base := initialState()
	base.HighWater = 3
	obj, e := copyObject(JSON{"nested": JSON{"v": "immutable"}}, l)
	if e != nil {
		t.Fatal(e)
	}
	base.Conversations[1] = Conversation{ID: 1}
	base.Documents[2] = Document{ID: 2, Scope: "session", Kind: "state", Version: 1, Value: obj, CreatedAt: 1}
	base.Entries[3] = Entry{ID: 3, Conversation: 1, Kind: "message", Value: obj, Seq: 1, Position: 1}
	base.Seq = 1
	candidate, e := prepare(base, commitRecord{Seq: 2, Writes: []Write{{Op: "retire-document", Document: &Document{ID: 2, Scope: "session", Kind: "state"}}}}, l)
	if e != nil {
		t.Fatal(e)
	}
	if base.Documents[2].Retired || !candidate.Documents[2].Retired || base.Seq != 1 {
		t.Fatal("candidate modified base record")
	}
	// Only the public clone may escape: its mutations cannot touch shared JSON in
	// either prior base or candidate. Preparation itself never mutates nested JSON.
	detached, e := cloneState(candidate, l)
	if e != nil {
		t.Fatal(e)
	}
	detached.Documents[2].Value["nested"].(map[string]any)["v"] = "external mutation"
	detached.Entries[3].Value["nested"].(map[string]any)["v"] = "external mutation"
	if base.Documents[2].Value["nested"].(map[string]any)["v"] != "immutable" || candidate.Entries[3].Value["nested"].(map[string]any)["v"] != "immutable" {
		t.Fatal("clone shallow shared private value")
	}
	_, e = prepare(base, commitRecord{Seq: 2, Writes: []Write{{Op: "retire-document", Document: &Document{ID: 2, Scope: "session", Kind: "state"}}, entry(4, JSON{})}}, l)
	rejected(t, e)
	if base.Documents[2].Retired {
		t.Fatal("rejected candidate mutated base")
	}
}

func TestIncrementalValidationMatchesColdValidationAndRetainedBudget(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		store := b.store
		id := mint(t, store)
		doc := historyDocument(id, 1, "cache.validation", "", "rewindable", "asOf")
		doc.Value = JSON{"xs": []any{}, "text": "base"}
		apply(t, store, putDocument(doc))
		for i := 0; i < 3; i++ {
			apply(t, store, deltaWrite(id, 1, Operation{"p", []any{"xs"}, i, 0, []any{i}}))
		}
		var core *storeCore
		if memory, ok := store.(*SQLiteStorage); ok {
			core = memory.storeCore
		} else {
			core = store.(*SQLiteStorage).storeCore
		}
		<-core.line
		warm := core.state
		core.leave()
		if warm.retained == nil {
			t.Fatal("confirmed state lacks private size cache")
		}
		cold, err := cloneState(warm, core.limits)
		if err != nil || cold.retained != nil {
			t.Fatal("public snapshot retained private cache", err)
		}
		retainedBytes := int64(0)
		for _, n := range warm.retained.records {
			retainedBytes += int64(n)
		}
		for _, sizes := range warm.retained.revisions {
			for _, n := range sizes {
				retainedBytes += int64(n)
			}
		}
		noop := warm.Documents[id]
		noop.Value = JSON{"xs": []any{0, 1, 2}, "text": "base"}
		for _, variant := range []string{"delta", "version-base", "invalid-path", "retire"} {
			t.Run(variant, func(t *testing.T) {
				write := deltaWrite(id, 1, Operation{"a", []any{"text"}, " extended"})
				switch variant {
				case "version-base":
					next := noop
					next.Version = 2
					next.Value = JSON{"xs": []any{}, "text": "new"}
					write = putDocument(next)
				case "invalid-path":
					write = deltaWrite(id, 1, Operation{"a", []any{"missing"}, "bad"})
				case "retire":
					write = Write{Op: "retire-document", Document: &noop}
				}
				record := commitRecord{Seq: warm.Seq + 1, Writes: []Write{write}}
				for _, restricted := range []bool{false, true} {
					limits := core.limits
					if restricted {
						limits.MaxRetainedBytes = retainedBytes
					}
					a, ae := prepare(warm, record, limits)
					b, be := prepare(cold, record, limits)
					if (ae == nil) != (be == nil) {
						t.Fatal("warm/cold acceptance differs", restricted, ae, be)
					}
					if ae == nil {
						a.retained, b.retained = nil, nil
						if !reflect.DeepEqual(a, b) {
							t.Fatal("warm/cold persisted state differs")
						}
					} else if ae.Error() != be.Error() {
						t.Fatal("warm/cold rejection differs", ae, be)
					}
				}
			})
		}
		// A public detached revision cannot grant cache authority on a later prepare.
		cold.DocumentRevisions[id][1].Version = 2
		if _, err := prepare(cold, commitRecord{Seq: cold.Seq + 1, Writes: []Write{putDocument(noop)}}, core.limits); err == nil {
			t.Fatal("cold malformed history accepted")
		}
	})
}

func newSQLiteTestStore(t *testing.T) (*SQLiteStorage, string) {
	t.Helper()
	dir := t.TempDir()
	store, err := openSQLiteTestDirectory(dir, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	seedRootConversation(t, store)
	return store, dir
}
