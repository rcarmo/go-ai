//go:build cgo

package durable

import (
	"context"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"path/filepath"
	"testing"
)

func TestSQLiteTransactionHistoryLimitsAndReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "session.sqlite")
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := OpenSQLite(path, SQLiteOptions{}); !errors.Is(err, ErrOwned) {
		if duplicate != nil {
			duplicate.Close(bg)
		}
		t.Fatal("SQLite shared ownership", err)
	}
	id, err := store.MintID(bg)
	if err != nil {
		t.Fatal(err)
	}
	doc := historyDocument(id, 1, "sqlite.notes", "", "rewindable", "asOf")
	doc.Value = JSON{"text": "base"}
	seq, err := store.Apply(bg, Batch{Writes: []Write{putDocument(doc)}})
	if err != nil {
		t.Fatal(err)
	}
	apply(t, store, deltaWrite(id, 1, Operation{"a", []any{"text"}, " tail"}))
	before := snap(t, store)
	if _, err := store.Apply(bg, Batch{Writes: []Write{deltaWrite(id, 1, Operation{"a", []any{"missing"}, "bad"})}}); err == nil {
		t.Fatal("SQLite invalid delta admitted")
	}
	after := snap(t, store)
	if before.Seq != after.Seq {
		t.Fatal("SQLite rejected write changed state")
	}
	if err := store.Reclaim(bg); err != nil {
		t.Fatal("SQLite reclaim", err)
	}
	if current := snap(t, store); current.Seq != before.Seq || current.HighWater != before.HighWater {
		t.Fatal("reclaim changed logical metadata")
	}
	if err := store.Close(bg); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close(bg)
	historical, ok, err := reopened.DocumentAt(bg, id, DocumentPoint{Seq: seq})
	if err != nil || !ok || historical.Value["text"] != "base" {
		t.Fatal(historical, ok, err)
	}
	current, ok, err := reopened.DocumentAt(bg, id, CurrentDocumentPoint())
	if err != nil || !ok || current.Value["text"] != "base tail" || current.DeltasSinceBase != 1 {
		t.Fatal(current, ok, err)
	}
	apply(t, reopened, deltaWrite(id, 1, Operation{"a", []any{"text"}, " after reclaim"}))
	if err := reopened.Reclaim(bg); err != nil {
		t.Fatal("second reclaim", err)
	}
	current, ok, err = reopened.DocumentAt(bg, id, CurrentDocumentPoint())
	if err != nil || !ok || current.Value["text"] != "base tail after reclaim" {
		t.Fatal("post-reclaim admission", current, err)
	}
}
func TestSQLiteHarnessGenerationAndTerminalReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "session.sqlite")
	calls := 0
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls++
		ch := make(chan goai.Event, 1)
		ch <- terminal("SQLite answer")
		close(ch)
		return ch
	})
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	first := openHarness(t, store, options)
	conversation := root(t, first, ref)
	sub, err := conversation.Submit(bg, Input{Content: "go"})
	if err != nil {
		t.Fatal(err)
	}
	result := waitSubmission(t, sub)
	if result.Submission.Status != "done" {
		t.Fatal(result)
	}
	if err := first.Close(bg); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	second := openHarness(t, reopened, options)
	handle, err := second.Submission(bg, sub.ID())
	if err != nil {
		t.Fatal(err)
	}
	if settled := waitSubmission(t, handle); settled.Submission.Status != "done" || calls != 1 {
		t.Fatal("SQLite replayed model", settled, calls)
	}
}
