package durable

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"
)

func sessionFor(t *testing.T, s Storage) *Session {
	t.Helper()
	session, e := OpenSession(s)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = session.Close(bg) })
	return session
}
func TestTransactionImmediateCopyAndSealed(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := sessionFor(t, b.store)
		doc, e := s.MintID(bg)
		if e != nil {
			t.Fatal(e)
		}
		var escaped *Tx
		var handle *DocumentHandle
		var candidate JSON
		input := JSON{"nested": JSON{"x": "original"}, "array": []any{JSON{"v": "original"}}}
		_, e = s.Commit(bg, func(tx *Tx) error {
			escaped = tx
			var err error
			handle, err = tx.CreateDocument(Document{ID: doc, Scope: "session", Kind: "app.state", Version: 1, Value: JSON{}})
			if err != nil {
				return err
			}
			if err = handle.Set(input); err != nil {
				return err
			}
			input["nested"].(JSON)["x"] = "mutated-before-commit"
			input["array"].([]any)[0].(JSON)["v"] = "mutated"
			got, err := handle.Get()
			if err != nil {
				return err
			}
			if got["nested"].(map[string]any)["x"] != "original" {
				t.Fatal("Set did not immediately copy")
			}
			got["nested"].(map[string]any)["x"] = "returned-copy"
			if err = handle.Update(func(v JSON) error {
				candidate = v
				v["updated"] = "committed"
				if _, err := handle.Get(); !errors.Is(err, ErrConcurrent) {
					t.Fatal("reentrant handle", err)
				}
				return nil
			}); err != nil {
				return err
			}
			candidate["updated"] = "mutated-after-update"
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
		input["nested"].(JSON)["x"] = "postcommit"
		candidate["updated"] = "postcommit"
		v, e := s.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		if v.Documents[doc].Value["updated"] != "committed" || v.Documents[doc].Value["nested"].(map[string]any)["x"] != "original" {
			t.Fatal(v.Documents[doc])
		}
		for _, f := range []func() error{func() error { return handle.Set(JSON{}) }, func() error { _, e := handle.Get(); return e }, func() error { return handle.Update(func(JSON) error { return nil }) }, handle.Retire, func() error { _, e := escaped.MintID(); return e }, func() error { return escaped.Unsupported("copy") }} {
			if e := f(); !errors.Is(e, ErrSealed) {
				t.Fatal(e)
			}
		}
		old := v.Documents[doc].Value
		_, e = s.Commit(bg, func(tx *Tx) error {
			h, e := tx.Document(doc)
			if e != nil {
				return e
			}
			return h.Set(JSON{"revision": 2})
		})
		if e != nil {
			t.Fatal(e)
		}
		if old["updated"] != "committed" {
			t.Fatal("old snapshot changed")
		}
	})
}

func TestTransactionCallbackInvalidAndRollback(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := sessionFor(t, b.store)
		id, e := s.MintID(bg)
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.Commit(bg, func(tx *Tx) error {
			_, e := tx.CreateDocument(Document{ID: id, Scope: "session", Kind: "state", Version: 1, Value: JSON{"x": 1}})
			return e
		})
		if e != nil {
			t.Fatal(e)
		}
		before, e := s.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		sentinel := errors.New("callback")
		_, e = s.Commit(bg, func(tx *Tx) error {
			h, e := tx.Document(id)
			if e != nil {
				return e
			}
			if e = h.Update(func(v JSON) error { v["x"] = 2; return sentinel }); !errors.Is(e, sentinel) {
				t.Fatal(e)
			}
			got, e := h.Get()
			if e != nil {
				return e
			}
			if got["x"] != json.Number("1") {
				t.Fatal(got)
			}
			if e = h.Set(JSON{"nan": math.NaN()}); e == nil {
				t.Fatal("NaN accepted")
			}
			return sentinel
		})
		if !errors.Is(e, sentinel) {
			t.Fatal(e)
		}
		after, e := s.Snapshot(bg)
		if e != nil || !equalSnapshot(before, after) {
			t.Fatal("failed callback visible", e)
		}
		var escaped *Tx
		func() {
			defer func() {
				if recover() == nil {
					t.Fatal("expected callback panic")
				}
			}()
			_, _ = s.Commit(bg, func(tx *Tx) error { escaped = tx; panic("test") })
		}()
		if _, e = escaped.MintID(); !errors.Is(e, ErrSealed) {
			t.Fatal(e)
		}
		_, e = s.Commit(bg, func(tx *Tx) error {
			return tx.AppendEntry(Entry{ID: ID(MaxID + 1), Conversation: 1, Kind: "message", Value: JSON{}})
		})
		rejected(t, e)
	})
}

var marshalCalls atomic.Int64

type hostileString string

func (hostileString) MarshalJSON() ([]byte, error) { marshalCalls.Add(1); return []byte(`{}`), nil }

type hostileMap map[string]any

func (hostileMap) MarshalJSON() ([]byte, error) { marshalCalls.Add(1); return []byte(`{}`), nil }

type hostileText string

func (hostileText) MarshalText() ([]byte, error) { marshalCalls.Add(1); return []byte("changed"), nil }
func TestNoCallerMarshalAuthority(t *testing.T) {
	for _, value := range []any{hostileString("payload"), hostileMap{"secret": "payload"}, []any{hostileText("nested")}, map[hostileText]any{"key": "value"}} {
		marshalCalls.Store(0)
		_, e := copyObject(JSON{"v": value}, DefaultLimits())
		rejected(t, e)
		if marshalCalls.Load() != 0 {
			t.Fatal("caller marshaler invoked")
		}
	}
	s, _ := NewMemory()
	h := sessionFor(t, s)
	id, _ := h.MintID(bg)
	_, e := h.Commit(bg, func(tx *Tx) error {
		doc, e := tx.CreateDocument(Document{ID: id, Scope: "session", Kind: "state", Version: 1, Value: JSON{}})
		if e != nil {
			return e
		}
		marshalCalls.Store(0)
		if e = doc.Set(JSON{"v": hostileString("x")}); e == nil {
			t.Fatal("hostileSet accepted")
		}
		if e = doc.Update(func(v JSON) error { v["v"] = hostileMap{}; return nil }); e == nil {
			t.Fatal("hostileUpdate accepted")
		}
		if marshalCalls.Load() != 0 {
			t.Fatal("marshaler called")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}

func TestRetainedStoreCannotBypassSession(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := sessionFor(t, b.store)
		if _, e := b.store.MintID(bg); !errors.Is(e, ErrOwned) {
			t.Fatal(e)
		}
		if _, e := b.store.Apply(bg, Batch{}); !errors.Is(e, ErrOwned) {
			t.Fatal(e)
		}
		if e := b.store.Close(bg); !errors.Is(e, ErrOwned) {
			t.Fatal(e)
		}
		entered := make(chan struct{})
		release := make(chan struct{})
		done := make(chan error, 1)
		go func() {
			_, e := s.Commit(bg, func(tx *Tx) error {
				close(entered)
				<-release
				id, e := tx.MintID()
				if e != nil {
					return e
				}
				return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "message", Value: JSON{"winner": true}})
			})
			done <- e
		}()
		<-entered
		if _, e := b.store.Apply(bg, Batch{}); !errors.Is(e, ErrOwned) {
			t.Fatal(e)
		}
		close(release)
		if e := <-done; e != nil {
			t.Fatal(e)
		}
		v, e := s.Snapshot(bg)
		if e != nil || len(v.Entries) != 1 {
			t.Fatal(v, e)
		}
	})
}

func TestAdmittedCancellationAndCloseFence(t *testing.T) {
	s, dir := newJournal(t)
	h := sessionFor(t, s)
	id, e := h.MintID(bg)
	if e != nil {
		t.Fatal(e)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	s.file = &faultFile{journalFile: s.file, writeLimit: -1, syncEntered: entered, release: release}
	ctx, cancel := context.WithCancel(bg)
	commit := make(chan error, 1)
	go func() {
		_, e := h.Commit(ctx, func(tx *Tx) error {
			return tx.AppendEntry(Entry{ID: id, Conversation: 1, Kind: "message", Value: JSON{"committed": true}})
		})
		commit <- e
	}()
	<-entered
	cancel()
	// A queued read cannot observe staged data or release the admitted line.
	readCtx, readCancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer readCancel()
	if _, e = h.Snapshot(readCtx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("read escaped barrier", e)
	}
	closeCtx, closeCancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer closeCancel()
	if e = h.Close(closeCtx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal("close failed to wait", e)
	}
	if _, e = OpenJournal(dir, JournalOptions{}); !errors.Is(e, ErrOwned) {
		t.Fatal("premature successor", e)
	}
	close(release)
	if e = <-commit; e != nil {
		t.Fatal("admitted canceled commit did not settle", e)
	}
	if e = h.Close(bg); e != nil {
		t.Fatal(e)
	}
	r, e := OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close(bg)
	if len(snap(t, r).Entries) != 1 {
		t.Fatal("admitted winner not adopted")
	}
}

func TestPublicTransactionForwardReferencesAtomic(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		s := sessionFor(t, b.store)
		conversation, _ := s.MintID(bg)
		task, _ := s.MintID(bg)
		doc, _ := s.MintID(bg)
		ent, _ := s.MintID(bg)
		_, e := s.Commit(bg, func(tx *Tx) error {
			if e := tx.CreateConversation(Conversation{ID: conversation, Owner: task}); e != nil {
				return e
			}
			h, e := tx.CreateDocument(Document{ID: doc, Scope: "task", Owner: task, Kind: "state", Version: 1, Value: JSON{"ready": true}})
			if e != nil {
				return e
			}
			if e = h.Set(JSON{"phase": "prepared"}); e != nil {
				return e
			}
			if e = tx.AppendEntry(Entry{ID: ent, Conversation: conversation, Kind: "message", Value: JSON{}}); e != nil {
				return e
			}
			return tx.PutTask(Task{ID: task, Conversation: conversation, Kind: "generation", Status: "pending", Checkpoint: JSON{"step": 1}})
		})
		if e != nil {
			t.Fatal(e)
		}
		v, e := s.Snapshot(bg)
		if e != nil || v.Seq != 1 || v.Tasks[task].Conversation != conversation || v.Documents[doc].Value["phase"] != "prepared" {
			t.Fatal(v, e)
		}
		// Final unresolved references reject the whole batch, but intermediate schema
		// failures remain immediate and leave the previous Set candidate intact.
		next, _ := s.MintID(bg)
		_, e = s.Commit(bg, func(tx *Tx) error { return tx.CreateConversation(Conversation{ID: next, Owner: next + 1}) })
		rejected(t, e)
		after, e := s.Snapshot(bg)
		if e != nil || after.Seq != v.Seq || len(after.Conversations) != len(v.Conversations) {
			t.Fatal("unresolved final adopted")
		}
	})
}

func TestStorageCloseSealsBeforeAdmittedWriteDrain(t *testing.T) {
	s, dir := newJournal(t)
	id := mint(t, s)
	entered, release := make(chan struct{}), make(chan struct{})
	s.file = &faultFile{journalFile: s.file, writeLimit: -1, syncEntered: entered, release: release}
	done := make(chan error, 1)
	go func() { _, e := s.Apply(bg, Batch{[]Write{entry(id, JSON{})}}); done <- e }()
	<-entered
	// A deadline waits for drain only; the close invocation already fenced future
	// admission before it waits for the blocked append settlement.
	ctx, cancel := context.WithTimeout(bg, 20*time.Millisecond)
	defer cancel()
	if e := s.Close(ctx); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatal(e)
	}
	if _, e := s.Apply(bg, Batch{}); !errors.Is(e, ErrClosed) {
		t.Fatal("lateApply admitted", e)
	}
	if _, e := s.MintID(bg); !errors.Is(e, ErrClosed) {
		t.Fatal("lateMint admitted", e)
	}
	if _, e := OpenJournal(dir, JournalOptions{}); !errors.Is(e, ErrOwned) {
		t.Fatal("premature file successor", e)
	}
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	if e := s.Close(bg); e != nil {
		t.Fatal(e)
	}
	r, e := OpenJournal(dir, JournalOptions{})
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close(bg)
	if len(snap(t, r).Entries) != 1 {
		t.Fatal("wrong drainwinner")
	}
}
