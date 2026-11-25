package durable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestDraftV160CodemodePublishedLargeStoreRecovery(t *testing.T) {
	const childDir = "GOAI_CODEMODE_R51_CRASH_DIR"
	sandbox, worker := newJournalWorkerSandboxV160(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	child := os.Getenv(childDir)
	registry := NewRegistry()
	def := taskDefinition(t, "store-loss-v160", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		result, err := r.RunCodemode(ctx, "write", v160FiveStoreValues, sandbox)
		if err != nil || !result.HasValue || result.Value != "stored-five" || len(result.StoreWrites) != 5 || result.Error != "" {
			return fmt.Errorf("large store result at process boundary: value=%v, writes=%d, error=%v", result.Value, len(result.StoreWrites), err)
		}
		for i := 0; i < 5; i++ {
			if result.StoreWrites[fmt.Sprintf("k%d", i)] != strings.Repeat(fmt.Sprint(i), 200<<10) {
				return fmt.Errorf("wrong full result for k%d", i)
			}
		}
		if child != "" {
			// Store, result and fragments have settled, but the owner remains
			// running. Do not close the Harness or finish the task.
			return syscall.Kill(os.Getpid(), syscall.SIGKILL)
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err := registry.RegisterTask(def); err != nil {
		t.Fatal(err)
	}
	open := func(dir string) *SQLiteStorage {
		t.Helper()
		store, err := openSQLiteTestDirectory(filepath.Join(dir, "sqlite"), SQLiteOptions{})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = store.Close(bg) })
		return store
	}
	if child != "" {
		h := openHarness(t, open(child), Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(ctx); err != nil {
			t.Fatal(err)
		}
		row, err := h.WaitForTask(ctx, id)
		t.Fatalf("child did not die after settled large store: %+v; %v", row.State.Outcome, err)
	}
	dir := t.TempDir()
	proc := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestDraftV160CodemodePublishedLargeStoreRecovery$")
	proc.Env = append(os.Environ(), childDir+"="+dir)
	output, err := proc.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL || ctx.Err() != nil {
		t.Fatalf("child did not die at store commit: %v\n%s", err, output)
	}
	store := open(dir)
	before, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var runDoc, part Document
	for _, d := range before.Documents {
		if d.Kind == codemodeRunKind && !d.Retired {
			runDoc = d
		}
		if d.Kind == codemodeStoreResultKind && d.Key == "write/0" {
			part = d
		}
	}
	if runDoc.ID == 0 || part.ID == 0 || runDoc.Owner != part.Owner || before.Tasks[runDoc.Owner].Status != "running" {
		t.Fatal("missing committed patch and running task at the process boundary")
	}
	refuse := func(write StorageWrite, want ...string) {
		t.Helper()
		n := 0
		result, err := store.Commit(ctx, [][]StorageWrite{{write}}, func(int, uint64) { n++ })
		if err != nil || n != 0 || result.Stored != 0 || result.Refused == nil {
			t.Fatalf("unvalidated %s: result=%+v receipts=%d err=%v", write.Type, result, n, err)
		}
		got := fmt.Sprint(result.Refused.Value())
		for _, text := range want {
			if strings.Contains(got, text) {
				return
			}
		}
		t.Fatalf("%s refused for wrong reason: %s; want one of %v", write.Type, got, want)
	}
	refuse(StorageWrite{Type: "document.change", ID: part.ID, Content: &DocumentContent{Version: 1, Kind: "base", Value: JSON{"key": "k0", "value": "forged"}}}, "codemode store part changed")
	refuse(StorageWrite{Type: "document.retire", ID: part.ID}, "invalid codemode store part reference", "invalid codemode store part retirement")
	orphan, err := store.MintID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	key := "write/5"
	refuse(StorageWrite{Type: "document.create", Record: &DocumentCreate{ID: orphan, Kind: codemodeStoreResultKind, Key: &key, Scope: DocumentCreateScope{Kind: "task", TaskID: runDoc.Owner}}, Content: &DocumentContent{Version: 1, Kind: "base", Value: JSON{"key": "k5", "value": "forged"}}}, "orphan codemode store part")
	after, err := store.Snapshot(ctx)
	if err != nil || after.Seq != before.Seq || after.Documents[orphan].ID != 0 || after.Documents[part.ID].Retired || !equalJSONValue(part.Value, after.Documents[part.ID].Value) {
		t.Fatal("refused storage command changed the committed prefix", err)
	}
	h := openHarness(t, store, Options{Registry: registry})
	if err := h.Resume(ctx); err != nil {
		t.Fatal(err)
	}
	row, err := h.WaitForTask(ctx, runDoc.Owner)
	if row.State.Outcome != nil && row.State.Outcome.Error != nil {
		t.Fatal(row.State.Outcome.Error.Message)
	}
	if err != nil || row.State.Outcome == nil || row.State.Outcome.Status != "completed" || worker.starts.Load() != 0 {
		t.Fatalf("reopen did not reuse the settled patch: %+v, starts=%d, err=%v", row.State.Outcome, worker.starts.Load(), err)
	}
}
