package durable

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

var bg = context.Background()

type backend struct {
	store  Storage
	reopen func() Storage
}

func backends(t *testing.T, fn func(*testing.T, backend)) {
	t.Helper()
	for _, name := range []string{"memory", "journal"} {
		t.Run(name, func(t *testing.T) {
			var open func() Storage
			if name == "memory" {
				image := &MemoryImage{}
				open = func() Storage {
					s, e := OpenMemory(MemoryOptions{Image: image})
					if e != nil {
						t.Fatal(e)
					}
					return s
				}
			} else {
				dir := filepath.Join(t.TempDir(), "private")
				open = func() Storage {
					s, e := OpenJournal(dir, JournalOptions{})
					if e != nil {
						t.Fatal(e)
					}
					return s
				}
			}
			s := open()
			t.Cleanup(func() { _ = s.Close(bg) })
			fn(t, backend{s, func() Storage {
				if e := s.Close(bg); e != nil {
					t.Fatal(e)
				}
				s = open()
				return s
			}})
		})
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
	backends(t, func(t *testing.T, b backend) {
		s := b.store
		t.Run("01_reserved_root_immutable", func(t *testing.T) {
			v := snap(t, s)
			if v.HighWater != 1 || len(v.Conversations) != 1 || v.Conversations[1].ID != 1 {
				t.Fatal(v)
			}
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
		if seq != 1 || v.Seq != 1 || v.Documents[doc].Value["\x00 key "].([]any)[0] != "original" {
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
			if len(snap(t, s).Entries) != 2 || snap(t, s).Seq != 1 {
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
			r, e := s.Tasks(bg, Query{Conversation: conv, Status: "completing", Kind: "generation", Limit: 1})
			if e != nil || len(r) != 1 || len(r[0].Checkpoint) != 1 {
				t.Fatal(r, e)
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
			checks := []func() error{func() error { _, e := s.MintID(bg); return e }, func() error { _, e := s.Apply(bg, Batch{}); return e }, func() error { _, e := s.Snapshot(bg); return e }, func() error { _, e := s.Limits(); return e }, func() error { _, e := s.Conversations(bg, Query{Limit: 1}); return e }, func() error { _, e := s.Entries(bg, 1, EntryCursor{}, 1); return e }, func() error { _, e := s.Tasks(bg, Query{Limit: 1}); return e }, func() error { _, e := s.Submissions(bg, Query{Limit: 1}); return e }, func() error { _, e := s.Documents(bg, Query{Limit: 1}); return e }, func() error { _, _, e := s.Request(bg, 1, "id"); return e }}
			for _, f := range checks {
				if e := f(); !errors.Is(e, ErrClosed) {
					t.Fatal(e)
				}
			}
		})
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
			s, _ := NewMemory()
			_, e = s.Apply(bg, Batch{[]Write{{Op: "create-conversation", Conversation: &c}}})
			_ = s.Close(bg)
		}
		rejected(t, e)
	}
}

type faultFile struct {
	journalFile
	writeLimit  int
	writeErr    error
	syncErr     error
	syncEntered chan struct{}
	release     chan struct{}
}

func (f *faultFile) Write(p []byte) (int, error) {
	if f.writeLimit >= 0 && f.writeLimit < len(p) {
		n, e := f.journalFile.Write(p[:f.writeLimit])
		if e != nil {
			return n, e
		}
		return n, f.writeErr
	}
	n, e := f.journalFile.Write(p)
	if f.writeErr != nil {
		return n, f.writeErr
	}
	return n, e
}
func (f *faultFile) Sync() error {
	if f.syncEntered != nil {
		close(f.syncEntered)
		<-f.release
		f.syncEntered = nil
	}
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.journalFile.Sync()
}
func newJournal(t *testing.T) (*JournalStorage, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	s, e := OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = s.Close(bg) })
	return s, dir
}
func TestJournalWriteSyncUncertain(t *testing.T) {
	for _, mode := range []string{"short", "enospc", "full-error", "sync"} {
		t.Run(mode, func(t *testing.T) {
			s, dir := newJournal(t)
			id := mint(t, s)
			f := &faultFile{journalFile: s.file, writeLimit: -1}
			switch mode {
			case "short":
				f.writeLimit = 17
			case "enospc":
				f.writeLimit = 0
				f.writeErr = syscall.ENOSPC
			case "full-error":
				f.writeErr = io.ErrUnexpectedEOF
			case "sync":
				f.syncErr = errors.New("sync failure")
			}
			s.file = f
			_, e := s.Apply(bg, Batch{[]Write{entry(id, JSON{"landed": true})}})
			if !errors.Is(e, ErrPoisoned) {
				t.Fatal(e)
			}
			if _, e = s.Snapshot(bg); !errors.Is(e, ErrPoisoned) {
				t.Fatal("poison exposed read", e)
			}
			if _, e = s.MintID(bg); !errors.Is(e, ErrPoisoned) {
				t.Fatal(e)
			}
			if e = s.Close(bg); e != nil {
				t.Fatal(e)
			}
			r, e := OpenJournal(dir, JournalOptions{})
			if e != nil {
				t.Fatal(e)
			}
			defer r.Close(bg)
			v := snap(t, r)
			want := 0
			if mode == "full-error" || mode == "sync" {
				want = 1
			}
			if len(v.Entries) != want || v.HighWater != uint64(id) {
				t.Fatal("uncertain reopen", v)
			}
		})
	}
}
func TestJournalEveryTornByteAndCorruption(t *testing.T) {
	s, dir := newJournal(t)
	id := mint(t, s)
	prefix, e := os.ReadFile(filepath.Join(dir, "journal.bin"))
	if e != nil {
		t.Fatal(e)
	}
	apply(t, s, entry(id, JSON{"text": "bounded"}))
	all, e := os.ReadFile(filepath.Join(dir, "journal.bin"))
	if e != nil {
		t.Fatal(e)
	}
	tail := all[len(prefix):]
	_ = s.Close(bg)
	for cut := 0; cut < len(tail); cut++ {
		testDir := filepath.Join(t.TempDir(), "private")
		if e = os.Mkdir(testDir, 0700); e != nil {
			t.Fatal(e)
		}
		p := append(append([]byte{}, prefix...), tail[:cut]...)
		if e = os.WriteFile(filepath.Join(testDir, "journal.bin"), p, 0600); e != nil {
			t.Fatal(e)
		}
		r, e := OpenJournal(testDir, JournalOptions{})
		if e != nil {
			t.Fatalf("legal final cut%d %v", cut, e)
		}
		if len(snap(t, r).Entries) != 0 {
			t.Fatal("torn adopted")
		}
		next := mint(t, r)
		if next != id+1 {
			t.Fatal("reservation reused")
		}
		_ = r.Close(bg)
	}
	for _, offset := range []int{0, 8, 10, 11, 16, 24, 32, 64, len(tail) - 1} {
		testDir := filepath.Join(t.TempDir(), "private")
		_ = os.Mkdir(testDir, 0700)
		p := append([]byte{}, all...)
		p[len(prefix)+offset] ^= 0xff
		_ = os.WriteFile(filepath.Join(testDir, "journal.bin"), p, 0600)
		r, e := OpenJournal(testDir, JournalOptions{})
		if r != nil {
			_ = r.Close(bg)
		}
		if !errors.Is(e, ErrCorrupt) {
			t.Fatalf("corruptionoffset%d %v", offset, e)
		}
	}
	// A mismatched legal-tail prefix and partial terminator fail closed.
	for _, tail := range [][]byte{[]byte("BAD"), append(append([]byte{}, all[len(prefix):len(all)-7]...), byte('X'))} {
		testDir := filepath.Join(t.TempDir(), "private")
		_ = os.Mkdir(testDir, 0700)
		_ = os.WriteFile(filepath.Join(testDir, "journal.bin"), append(append([]byte{}, prefix...), tail...), 0600)
		_, e := OpenJournal(testDir, JournalOptions{})
		if !errors.Is(e, ErrCorrupt) {
			t.Fatal(e)
		}
	}
}
func TestJournalStrictReplayAndRelations(t *testing.T) {
	cases := []struct {
		name          string
		typ           byte
		ordinal, high uint64
		payload       string
	}{{"duplicate", 1, 2, 2, `{"first":2,"last":2,"last":2}`}, {"fraction", 1, 2, 2, `{"first":2.0,"last":2}`}, {"unreserved", 2, 2, 1, `{"seq":1,"writes":[{"op":"append-entry","entry":{"id":2,"conversation":1,"kind":"message","value":{}}}]}`}, {"unknown", 1, 2, 2, `{"first":2,"last":2,"secret":"ignored?"}`}, {"seqgap", 2, 2, 1, `{"seq":2,"writes":[]}`}, {"reservationgap", 1, 2, 3, `{"first":3,"last":3}`}, {"maxplus", 1, 2, MaxID + 1, `{"first":2,"last":9007199254740992}`}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, dir := newJournal(t)
			_ = s.Close(bg)
			f, e := os.OpenFile(filepath.Join(dir, "journal.bin"), os.O_WRONLY|os.O_APPEND, 0600)
			if e != nil {
				t.Fatal(e)
			}
			_, e = f.Write(frame(tc.typ, tc.ordinal, tc.high, []byte(tc.payload)))
			if e != nil {
				t.Fatal(e)
			}
			_ = f.Close()
			_, e = OpenJournal(dir, JournalOptions{})
			if !errors.Is(e, ErrCorrupt) {
				t.Fatal(e)
			}
		})
	}
}
func TestJournalNumericAndPolicyBounds(t *testing.T) {
	s, dir := newJournal(t)
	if _, e := OpenJournal(dir, JournalOptions{}); !errors.Is(e, ErrOwned) {
		t.Fatal(e)
	}
	s.state.HighWater = MaxID - 1
	s.ordinal = MaxID - 1
	id, e := s.MintID(bg)
	if e != nil || uint64(id) != MaxID {
		t.Fatal(id, e)
	}
	before := s.size
	_, e = s.MintID(bg)
	rejected(t, e)
	if s.size != before || s.poisoned {
		t.Fatal("exhaustion admitted")
	}
	_ = s.Close(bg)
	s, dir = newJournal(t)
	_ = s.Close(bg)
	syncFalse := false
	_, e = OpenJournal(dir, JournalOptions{Sync: &syncFalse})
	rejected(t, e)
	limits := DefaultLimits()
	limits.MaxDepth--
	_, e = OpenJournal(dir, JournalOptions{Limits: &limits})
	rejected(t, e)
	limits = DefaultLimits()
	limits.MaxFramePayloadBytes = 16<<20 + 1
	_, e = OpenMemory(MemoryOptions{Limits: &limits})
	rejected(t, e)
	// Invalid length is rejected from header before allocating payload bytes.
	testDir := filepath.Join(t.TempDir(), "private")
	_ = os.Mkdir(testDir, 0700)
	h := frame(0, 1, 1, []byte(`{}`))
	binary.LittleEndian.PutUint32(h[12:16], ^uint32(0))
	_ = os.WriteFile(filepath.Join(testDir, "journal.bin"), h[:64], 0600)
	_, e = OpenJournal(testDir, JournalOptions{})
	if !errors.Is(e, ErrCorrupt) {
		t.Fatal(e)
	}
}
func TestJournalParentDirectorySyncFailure(t *testing.T) {
	old := syncDirectory
	defer func() { syncDirectory = old }()
	calls := 0
	syncDirectory = func(string) error { calls++; return errors.New("directory sync failure") }
	_, e := OpenJournal(filepath.Join(t.TempDir(), "new", "nested"), JournalOptions{})
	if !errors.Is(e, ErrPoisoned) || calls != 1 {
		t.Fatal(e, calls)
	}
}

func TestLimitsPreAdmissionBothBackends(t *testing.T) {
	for _, name := range []string{"MaxDocumentBytes", "MaxRecordBytes", "MaxFramePayloadBytes", "MaxStringBytes", "MaxRequestIDBytes", "MaxDepth", "MaxMembers", "MaxNodes", "MaxWrites", "MaxPage", "MaxRecords", "MaxRetainedBytes", "MaxJournalBytes"} {
		t.Run(name, func(t *testing.T) {
			for _, kind := range []string{"memory", "journal"} {
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
					case "MaxJournalBytes":
						l.MaxFramePayloadBytes = 512
						l.MaxRecordBytes = 256
						l.MaxDocumentBytes = 128
						l.MaxStringBytes = 128
						l.MaxRequestIDBytes = 32
						l.MaxJournalBytes = 1100
					}
					var s Storage
					var e error
					var j *JournalStorage
					if kind == "memory" {
						s, e = OpenMemory(MemoryOptions{Limits: &l})
					} else {
						j, e = OpenJournal(filepath.Join(t.TempDir(), "private"), JournalOptions{Limits: &l})
						s = j
					}
					if e != nil {
						t.Fatal(e)
					}
					defer s.Close(bg)
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
						ws = []Write{entry(id, JSON{"v": strings.Repeat("x", 17)})}
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
					case "MaxJournalBytes":
						if j == nil {
							return
						}
						j.size = l.MaxJournalBytes - 73
					}
					before := snap(t, s)
					size := int64(0)
					if j != nil {
						size = j.size
					}
					_, e = s.Apply(bg, Batch{ws})
					rejected(t, e)
					after := snap(t, s)
					if !equalSnapshot(before, after) {
						t.Fatal("rejected limit changed state")
					}
					if j != nil && (j.size != size || j.poisoned) {
						t.Fatal("limit admitted or poisoned")
					}
				})
			}
		})
	}
}

func TestJournalCreationSyncTailAndFirstPrefix(t *testing.T) {
	t.Run("config_file_sync_failure_uncertain", func(t *testing.T) {
		old := openJournalFile
		defer func() { openJournalFile = old }()
		openJournalFile = func(path string) (journalFile, error) {
			f, e := old(path)
			if e != nil {
				return nil, e
			}
			return &faultFile{journalFile: f, writeLimit: -1, syncErr: errors.New("sync failure")}, nil
		}
		dir := filepath.Join(t.TempDir(), "private")
		_, e := OpenJournal(dir, JournalOptions{})
		if !errors.Is(e, ErrPoisoned) {
			t.Fatal(e)
		}
		openJournalFile = old
		s, e := OpenJournal(dir, JournalOptions{})
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close(bg)
		if snap(t, s).Seq != 0 {
			t.Fatal("creation invented commit")
		}
	})
	t.Run("new_file_directory_sync_failure", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "private")
		_ = os.Mkdir(dir, 0700)
		old := syncDirectory
		defer func() { syncDirectory = old }()
		syncDirectory = func(string) error { return errors.New("leaf sync failure") }
		_, e := OpenJournal(dir, JournalOptions{})
		if !errors.Is(e, ErrPoisoned) {
			t.Fatal(e)
		}
	})
	t.Run("all_legal_first_header_prefixes", func(t *testing.T) {
		payload, e := encodeBounded(journalConfig{Version: 1, Sync: true, Limits: DefaultLimits()}, DefaultLimits(), 16384)
		if e != nil {
			t.Fatal(e)
		}
		f := frame(0, 1, 1, payload)
		for cut := 1; cut < 64; cut++ {
			dir := filepath.Join(t.TempDir(), "private")
			_ = os.Mkdir(dir, 0700)
			_ = os.WriteFile(filepath.Join(dir, "journal.bin"), f[:cut], 0600)
			s, e := OpenJournal(dir, JournalOptions{})
			if e != nil {
				t.Fatalf("firstcut%d: %v", cut, e)
			}
			_ = s.Close(bg)
		}
	})
	t.Run("sync_false_persisted_default_reopen", func(t *testing.T) {
		flag := false
		dir := filepath.Join(t.TempDir(), "private")
		s, e := OpenJournal(dir, JournalOptions{Sync: &flag})
		if e != nil {
			t.Fatal(e)
		}
		mint(t, s)
		_ = s.Close(bg)
		s, e = OpenJournal(dir, JournalOptions{})
		if e != nil {
			t.Fatal(e)
		}
		defer s.Close(bg)
		if s.syncWrites {
			t.Fatal("policy changed")
		}
	})
}

// Actual child-process death validates the reservation/complete-frame/torn-tail
// boundaries. Model/tool effect crash points belong to M1b/c, not this kernel.
func TestJournalProcessCrash(t *testing.T) {
	if dir := os.Getenv("GOAI_M1A_CRASH_DIR"); dir != "" {
		s, e := OpenJournal(dir, JournalOptions{})
		if e != nil {
			os.Exit(91)
		}
		id, e := s.MintID(bg)
		if e != nil {
			os.Exit(92)
		}
		switch os.Getenv("GOAI_M1A_CRASH_PHASE") {
		case "reserved":
		case "committed":
			if _, e = s.Apply(bg, Batch{[]Write{entry(id, JSON{"process": "winner"})}}); e != nil {
				os.Exit(93)
			}
		case "torn":
			p, _ := encodeBounded(commitRecord{Seq: 1, Writes: []Write{entry(id, JSON{})}}, s.limits, s.limits.MaxFramePayloadBytes)
			f := frame(2, 3, uint64(id), p)
			_, _ = s.file.Write(f[:70])
			_ = s.file.Sync()
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		os.Exit(94)
	}
	for _, phase := range []string{"reserved", "committed", "torn"} {
		t.Run(phase, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "private")
			cmd := exec.Command(os.Args[0], "-test.run=^TestJournalProcessCrash$")
			cmd.Env = append(os.Environ(), "GOAI_M1A_CRASH_DIR="+dir, "GOAI_M1A_CRASH_PHASE="+phase)
			if e := cmd.Run(); e == nil {
				t.Fatal("child did not crash")
			}
			s, e := OpenJournal(dir, JournalOptions{})
			if e != nil {
				t.Fatal(e)
			}
			defer s.Close(bg)
			v := snap(t, s)
			want := 0
			if phase == "committed" {
				want = 1
			}
			if v.HighWater != 2 || len(v.Entries) != want {
				t.Fatal(v)
			}
			if mint(t, s) != 3 {
				t.Fatal("reservation reused after process death")
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
	s, e := OpenJournal(dir, JournalOptions{Limits: &l})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Close(bg); e != nil {
		t.Fatal(e)
	}
	s, e = OpenJournal(dir, JournalOptions{})
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
	_, e = OpenJournal(notCreated, JournalOptions{Limits: &invalid})
	rejected(t, e)
	if _, e = os.Stat(notCreated); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("invalid profile created directory")
	}
}

func TestSequenceAndOrdinalExhaustionBeforeIO(t *testing.T) {
	for _, kind := range []string{"seq", "ordinal"} {
		t.Run(kind, func(t *testing.T) {
			s, dir := newJournal(t)
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
			before := s.size
			_, e := s.Apply(bg, Batch{[]Write{entry(id, JSON{})}})
			rejected(t, e)
			if s.size != before || s.poisoned {
				t.Fatal("exhaustion admitted")
			}
		})
	}
}

func TestPublicLowLevelJSONAndEnvelopeBounds(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := b.store
		id := mint(t, s)
		marshalCalls.Store(0)
		for _, value := range []any{struct{ Value string }{"no implicit struct"}, hostileString("noMarshal"), json.Number(strings.Repeat("1", 1<<20))} {
			_, e := s.Apply(bg, Batch{[]Write{entry(id, JSON{"v": value})}})
			rejected(t, e)
		}
		if marshalCalls.Load() != 0 || snap(t, s).Seq != 0 {
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
	s, e := OpenMemory(MemoryOptions{Limits: &l})
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close(bg)
	var ws []Write
	for k := 0; k < 5; k++ {
		ws = append(ws, entry(mint(t, s), JSON{"nul": strings.Repeat("\x00", 30)}))
	}
	_, e = s.Apply(bg, Batch{ws})
	rejected(t, e)
	if snap(t, s).Seq != 0 {
		t.Fatal("frame split/partialadoption")
	}
}

func TestPrivateCandidateCOWPreservesPublicDetachAndRollback(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
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
		ts, e := s.Tasks(bg, Query{Limit: 1})
		if e != nil {
			t.Fatal(e)
		}
		ts[0].Checkpoint["nested"].(map[string]any)["v"] = "query mutate"
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
		var reopened Storage
		switch native := s.(type) {
		case *MemoryStorage:
			reopened, e = OpenMemory(MemoryOptions{Image: native.image})
		case *JournalStorage:
			reopened, e = OpenJournal(filepath.Dir(native.path), JournalOptions{})
		}
		if e != nil {
			t.Fatal(e)
		}
		defer reopened.Close(bg)
		if !equalSnapshot(authoritative, snap(t, reopened)) {
			t.Fatal("memory image/journal ownership changed")
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
