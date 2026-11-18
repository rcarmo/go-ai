package durable

import (
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestSourceV152ReadOnlyHarnessViewsRunningWork(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	storage, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reached := make(chan struct{})
	ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
		ch := make(chan goai.Event)
		close(reached)
		go func() { <-ctx.Done(); close(ch) }()
		return ch
	})
	writer := openHarness(t, storage, options)
	driverRoot := root(t, writer, ref)
	if _, err := driverRoot.Submit(bg, Input{Type: "input", Content: "slow"}); err != nil {
		t.Fatal(err)
	}
	awaitTaskSignal(t, reached)
	before, err := writer.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	var running ID
	for _, r := range before.Tasks {
		if r.Kind == "pi.generation" && r.Status == "running" {
			running = r.ID
			break
		}
	}
	if running == 0 {
		t.Fatal("writer reached provider without a stored running task")
	}
	reader, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(bg)
	var wakes atomic.Int64
	options.ReadOnly = true
	options.OnWake = func(Wake) { wakes.Add(1) }
	viewer, err := Open(bg, reader, options)
	if err != nil {
		t.Fatalf("open read-only Harness without recovering writer's task: %v", err)
	}
	defer viewer.Close(bg)
	viewRoot, err := viewer.Root(bg, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	page, err := viewRoot.Entries(bg, EntryQuery{}, 10, nil)
	if err != nil || len(page.Items) != 1 || page.Items[0].Kind != "pi.user" {
		t.Fatal(page, err)
	}
	live, found, err := viewRoot.SnapshotDefinition(bg, LiveDoc(), nil)
	if err != nil || !found {
		t.Fatal(live, found, err)
	}
	value, ok := live.Copy().(map[string]any)
	if !ok || value["run"] == nil {
		t.Fatal("running task absent from LiveDoc", live.Copy())
	}
	task, found, err := viewer.Task(bg, running)
	if err != nil || !found || task.State.Status != "running" || task.Restarts != 0 {
		t.Fatal(task, found, err)
	}
	inspection, err := viewer.Inspect(bg)
	if err != nil || inspection.Scheduling != "paused" {
		t.Fatal(inspection, err)
	}
	wake, err := viewer.Wake(bg)
	if err != nil || wake.Active || wake.At != nil {
		t.Fatal(wake, err)
	}
	checks := []func() error{
		func() error {
			_, err := viewRoot.Commit(bg, func(tx *Tx) (any, error) { return tx.AppendEntry(viewRoot.ID(), EntryDraft{"kind": "app.note"}) })
			return err
		},
		func() error { _, err := viewRoot.Submit(bg, Input{Type: "input", Content: "again"}); return err },
		func() error { return viewer.Resume(bg) },
		func() error { return viewer.WaitForIdle(bg) },
		func() error { return viewRoot.Abort(bg) },
		func() error { _, err := viewer.CreateConversation(bg, AgentChange{}); return err },
	}
	for i, run := range checks {
		var e *ReadOnlySession
		if got := run(); !errors.As(got, &e) {
			t.Fatalf("read-only operation %d: %v", i, got)
		}
	}
	if got, err := viewRoot.Commit(bg, func(*Tx) (any, error) { return "nothing", nil }); err != nil || got != "nothing" {
		t.Fatal(got, err)
	}
	after, err := writer.Snapshot(bg)
	if err != nil || after.Seq != before.Seq || after.HighWater != before.HighWater {
		t.Fatal("viewer changed the stored head", after.Seq, after.HighWater, err)
	}
	if _, err := driverRoot.Commit(bg, func(tx *Tx) (any, error) { return tx.AppendEntry(driverRoot.ID(), EntryDraft{"kind": "app.note"}) }); err != nil {
		t.Fatal(err)
	}
	page, err = viewRoot.Entries(bg, EntryQuery{}, 10, nil)
	if err != nil || len(page.Items) != 2 || page.Items[0].Kind != "app.note" {
		t.Fatal(page, err)
	}
	if err := viewer.Close(bg); err != nil {
		t.Fatal(err)
	}
	if wakes.Load() != 0 {
		t.Fatal("read-only viewer notified wake", wakes.Load())
	}
}

func TestSourceV152ReadOnlyHarnessLeavesEmptyRootAbsent(t *testing.T) {
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	view, err := Open(bg, store, Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close(bg)
	var readOnly *ReadOnlySession
	if _, err := view.Root(bg, AgentChange{}); !errors.As(err, &readOnly) {
		t.Fatal(err)
	}
	if got, err := view.Commit(bg, func(*Tx) (any, error) { return "ok", nil }); err != nil || got != "ok" {
		t.Fatal(got, err)
	}
	snapshot, err := view.Snapshot(bg)
	if err != nil || snapshot.Seq != 0 || snapshot.HighWater != 1 || len(snapshot.Conversations) != 0 {
		t.Fatal(snapshot, err)
	}
}
