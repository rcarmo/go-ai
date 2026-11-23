package durable

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

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
	beforeHead := readOnlyHarnessSQLiteHead(t, reader)
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
	taskID := readOnlyHarnessRunID(t, viewRoot)
	if taskID != running {
		t.Fatalf("LiveDoc names task %d, writer has running task %d", taskID, running)
	}
	assertReadOnlyHarnessTask(t, viewer, taskID, "running", 0)
	inspection, err := viewer.Inspect(bg)
	if err != nil || inspection.Scheduling != "paused" {
		t.Fatal(inspection, err)
	}
	wake, err := viewer.Wake(bg)
	if err != nil || wake.Active || wake.At != nil {
		t.Fatal(wake, err)
	}
	assertReadOnlyHarnessRefusals(t, viewer, viewRoot)
	after, err := writer.Snapshot(bg)
	if err != nil || after.Seq != before.Seq || after.HighWater != before.HighWater {
		t.Fatal("viewer changed the stored head", after.Seq, after.HighWater, err)
	}
	if next := readOnlyHarnessSQLiteHead(t, reader); next != beforeHead {
		t.Fatalf("running session head %d, want %d", next, beforeHead)
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
	if next := readOnlyHarnessSQLiteHead(t, storage); next != beforeHead+1 || wakes.Load() != 0 {
		t.Fatalf("after viewer close: head %d (one writer commit, want %d), wakes %d (want 0)", next, beforeHead+1, wakes.Load())
	}
}

func readOnlyHarnessRunID(t *testing.T, c *ConversationHandle) ID {
	t.Helper()
	live, found, err := c.SnapshotDefinition(bg, LiveDoc(), nil)
	if err != nil || !found {
		t.Fatal(live, found, err)
	}
	value, _ := live.Copy().(map[string]any)
	run, _ := value["run"].(map[string]any)
	id := submissionEntryID(run["taskId"])
	if id == 0 {
		t.Fatal("LiveDoc has no run task ID", run)
	}
	return id
}

func assertReadOnlyHarnessTask(t *testing.T, h *Harness, id ID, status string, restarts uint64) {
	t.Helper()
	task, found, err := h.Task(bg, id)
	if err != nil || !found || task.ID != id || task.State.Status != status || task.Restarts != restarts {
		t.Fatalf("task %d: id=%d found=%t status=%q restarts=%d err=%v; want status=%q restarts=%d", id, task.ID, found, task.State.Status, task.Restarts, err, status, restarts)
	}
}

// Read SQLite's next commit sequence independently of the Harness projection,
// like head() in the pinned upstream oracle.
func readOnlyHarnessSQLiteHead(t *testing.T, store *SQLiteStorage) uint64 {
	t.Helper()
	var next uint64
	if err := store.db.QueryRowContext(bg, "SELECT next_seq FROM durable_metadata WHERE singleton = 1").Scan(&next); err != nil {
		t.Fatal(err)
	}
	return next
}

// pi v1.6.0 harness-read-only.test.ts:126-154. Use writable Storage here,
// like the Oracle, to isolate the Harness-level read-only guard.
func TestSourceV160ReadOnlyHarnessRefusalsPreserveSettledSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.sqlite")
	var calls atomic.Int64
	ref, options := setupFakeAPI(t, func(context.Context, *goai.Model, *goai.Context, *goai.StreamOptions) <-chan goai.Event {
		calls.Add(1)
		ch := make(chan goai.Event, 1)
		ch <- terminal("one")
		close(ch)
		return ch
	})
	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	writer, err := Open(bg, store, options)
	if err != nil {
		_ = store.Close(bg)
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Close(bg) })
	c := root(t, writer, ref)
	sub, err := c.Submit(bg, Input{Type: "input", Content: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	waitSubmission(t, sub)
	if err := writer.Close(bg); err != nil {
		t.Fatal(err)
	}

	reader, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(bg)
	before := readOnlyHarnessSQLiteHead(t, reader)
	var reports atomic.Int64
	options.ReadOnly = true
	options.OnReport = func(error) { reports.Add(1) }
	viewer, err := Open(bg, reader, options)
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close(bg)
	c, err = viewer.Root(bg, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	assertReadOnlyHarnessRefusals(t, viewer, c)
	page, err := c.Entries(bg, EntryQuery{Order: Ascending}, 10, nil)
	if err != nil || len(page.Items) != 2 || page.Next != nil || page.Items[0].Kind != "pi.user" || page.Items[1].Kind != "pi.assistant" {
		t.Fatal(page, err)
	}
	if err := viewer.Close(bg); err != nil {
		t.Fatal(err)
	}
	after, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer after.Close(bg)
	if next := readOnlyHarnessSQLiteHead(t, after); next != before || reports.Load() != 0 || calls.Load() != 1 {
		t.Fatalf("after viewer close: head %d (want %d), reports %d (want 0), provider calls %d (want 1)", next, before, reports.Load(), calls.Load())
	}
}

func assertReadOnlyHarnessRefusals(t *testing.T, viewer *Harness, c *ConversationHandle) {
	t.Helper()
	checks := []struct {
		name string
		run  func() error
	}{
		{"write commit", func() error {
			_, err := c.Commit(bg, func(tx *Tx) (any, error) { return tx.AppendEntry(c.ID(), EntryDraft{"kind": "app.note"}) })
			return err
		}},
		{"submit", func() error { _, err := c.Submit(bg, Input{Type: "input", Content: "again"}); return err }},
		{"resume", func() error { return viewer.Resume(bg) }},
		{"wait for idle", func() error { return viewer.WaitForIdle(bg) }},
		{"abort", func() error { return c.Abort(bg) }},
		{"create conversation", func() error { _, err := viewer.CreateConversation(bg, AgentChange{}); return err }},
	}
	for _, check := range checks {
		var readOnly *ReadOnlySession
		if err := check.run(); !errors.As(err, &readOnly) {
			t.Errorf("%s: %v, want ReadOnlySession", check.name, err)
		}
	}
	if got, err := c.Commit(bg, func(*Tx) (any, error) { return "nothing", nil }); err != nil || got != "nothing" {
		t.Fatal(got, err)
	}
}

// pi v1.6.0 harness-read-only.test.ts:167-182. The native storage owns a
// single writer lock. The paused second writer therefore follows a real
// process loss rather than opening concurrently as the Node adapter does.
func TestSourceV160ReadOnlyHarnessLeavesRecoveryToWriter(t *testing.T) {
	const driverPath = "GO_AI_READ_ONLY_DRIVER_PATH"
	if path := os.Getenv(driverPath); path != "" {
		reached := make(chan struct{})
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, _ *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event)
			go func() { <-ctx.Done(); close(ch) }()
			close(reached)
			return ch
		})
		store, err := OpenSQLite(path, SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		h, err := Open(bg, store, options)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := root(t, h, ref).Submit(bg, Input{Type: "input", Content: "slow"}); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, reached)
		if _, err := fmt.Fprint(os.Stdout, "ready\n"); err != nil {
			t.Fatal(err)
		}
		var unused [1]byte
		_, err = os.Stdin.Read(unused[:])
		t.Fatal("driver was not killed", err)
	}

	path := filepath.Join(t.TempDir(), "session.sqlite")
	ctx, cancel := context.WithTimeout(bg, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestSourceV160ReadOnlyHarnessLeavesRecoveryToWriter$")
	cmd.Env = append(os.Environ(), driverPath+"="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	joined := false
	defer func() {
		if !joined {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	ready := make([]byte, len("ready\n"))
	if _, err := io.ReadFull(stdout, ready); err != nil || string(ready) != "ready\n" {
		t.Fatalf("driver receipt %q: %v", ready, err)
	}

	// Supplemental SQLite-only non-writing control. SHM is a shared lock
	// index, not a data file; it is not compared or used as JSONL evidence.
	dataFiles := []string{path, path + "-wal"}
	fileBytes := make([][]byte, len(dataFiles))
	for i, name := range dataFiles {
		var err error
		fileBytes[i], err = os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
	}
	reader, err := OpenSQLite(path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(bg)
	before := readOnlyHarnessSQLiteHead(t, reader)
	viewer, err := Open(bg, reader, Options{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close(bg)
	c, err := viewer.Root(bg, AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	id := readOnlyHarnessRunID(t, c)
	assertReadOnlyHarnessTask(t, viewer, id, "running", 0)
	if err := viewer.Close(bg); err != nil {
		t.Fatal(err)
	}
	for i, name := range dataFiles {
		after, err := os.ReadFile(name)
		if err != nil || !bytes.Equal(fileBytes[i], after) {
			t.Fatalf("viewer changed SQLite data bytes of %s: %v", filepath.Base(name), err)
		}
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	joined = true
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		t.Fatal("driver did not crash", err)
	}
	if status, ok := exit.Sys().(syscall.WaitStatus); !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL || ctx.Err() != nil {
		t.Fatal("driver did not crash at the gated step", exit, status, ctx.Err())
	}

	store, err := OpenSQLite(path, SQLiteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(bg)
	if next := readOnlyHarnessSQLiteHead(t, store); next != before {
		t.Fatalf("viewer or writer loss changed head: %d, want %d", next, before)
	}
	var calls atomic.Int64
	writer, err := Open(bg, store, Options{Models: func(goai.Provider, string) *goai.Model {
		calls.Add(1)
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close(bg)
	assertReadOnlyHarnessTask(t, writer, id, "pending", 1)
	if next := readOnlyHarnessSQLiteHead(t, store); next != before+1 || calls.Load() != 0 {
		t.Fatalf("recovery head %d (want %d); model lookups %d (want 0)", next, before+1, calls.Load())
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
	if _, err := view.Root(bg, AgentChange{Model: ModelRef{Provider: goai.ProviderOpenAI, ID: "durable-test"}}); !errors.As(err, &readOnly) {
		t.Fatal(err)
	}
	if got, err := view.Commit(bg, func(*Tx) (any, error) { return "ok", nil }); err != nil || got != "ok" {
		t.Fatal(got, err)
	}
	snapshot, err := view.Snapshot(bg)
	if err != nil || snapshot.Seq != 0 || snapshot.HighWater != 1 || len(snapshot.Conversations) != 0 {
		t.Fatal(snapshot, err)
	}
	if err := view.Close(bg); err != nil {
		t.Fatal(err)
	}
	reader, err := OpenSQLite(store.path, SQLiteOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(bg)
	if next := readOnlyHarnessSQLiteHead(t, reader); next != 1 {
		t.Fatalf("empty session head %d, want 1", next)
	}
}
