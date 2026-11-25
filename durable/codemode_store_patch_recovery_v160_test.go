package durable

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestV160CodemodePublishedLargeStoreRecovery(t *testing.T) {
	const childDir = "GOAI_CODEMODE_R51_CRASH_DIR"
	sandbox, worker := newJournalWorkerSandboxV160(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	child := os.Getenv(childDir)
	registry := NewRegistry()
	def := taskDefinition(t, "store-loss-v160", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		if child == "" {
			// Change the live store before reading the old run. The saved
			// result must not come from, or be reapplied to, the live map.
			mutation, err := r.RunCodemode(ctx, "mutate", `store("k0", "changed"); store("k1", undefined); return "mutated"`, sandbox)
			if err != nil || mutation.Error != "" || !mutation.HasValue || mutation.Value != "mutated" || len(mutation.StoreWrites) != 1 || mutation.StoreWrites["k0"] != "changed" || !reflect.DeepEqual(mutation.StoreDeletes, []string{"k1"}) || worker.starts.Load() != 1 {
				return fmt.Errorf("live store mutation: value=%v error=%q writes=%d deletes=%v starts=%d; %v", mutation.Value, mutation.Error, len(mutation.StoreWrites), mutation.StoreDeletes, worker.starts.Load(), err)
			}
		}
		result, err := r.RunCodemode(ctx, "write", v160FiveStoreValues, sandbox)
		if err != nil || !result.HasValue || result.Value != "stored-five" || len(result.StoreWrites) != 5 || len(result.StoreDeletes) != 0 || result.Error != "" || worker.starts.Load() != 1 {
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
		fresh, err := r.RunCodemode(ctx, "read", `return [load("k0"), load("k1") === undefined, [2,3,4].every(i => load("k"+i) === String(i).repeat(200*1024))]`, sandbox)
		if err != nil || fresh.Error != "" || !fresh.HasValue || !reflect.DeepEqual(fresh.Value, []any{"changed", true, true}) || len(fresh.StoreWrites) != 0 || worker.starts.Load() != 2 {
			return fmt.Errorf("fresh store read: value=%v error=%q writes=%d starts=%d; %v", fresh.Value, fresh.Error, len(fresh.StoreWrites), worker.starts.Load(), err)
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
	proc := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestV160CodemodePublishedLargeStoreRecovery$")
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
	partCount, partBytes := 0, 0
	for _, d := range before.Documents {
		if d.Kind == codemodeRunKind && !d.Retired {
			runDoc = d
		}
		if d.Kind == codemodeStoreResultKind {
			encoded, err := encodeBounded(d.Value, DefaultLimits(), DefaultLimits().MaxDocumentBytes)
			if err != nil {
				t.Fatalf("stored patch exceeded the unchanged document limit: %v", err)
			}
			partCount++
			partBytes += len(encoded)
			if d.Key == "write/0" {
				part = d
			}
		}
	}
	if runDoc.ID == 0 || part.ID == 0 || runDoc.Owner != part.Owner || before.Tasks[runDoc.Owner].Status != "running" {
		t.Fatal("missing committed patch and running task at the process boundary")
	}
	if partCount != 5 || partBytes <= DefaultLimits().MaxRecordBytes {
		t.Fatalf("result parts=%d bytes=%d; want five bounded parts exceeding one record in total", partCount, partBytes)
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
	refuse(StorageWrite{Type: "document.change", ID: part.ID, Content: &DocumentContent{Version: 1, Kind: "delta", Ops: []Operation{{"s", []any{"value"}, "forged"}}}}, "codemode store part changed")
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
	if err != nil || row.State.Outcome == nil || row.State.Outcome.Status != "completed" || worker.starts.Load() != 2 {
		t.Fatalf("reopen did not reuse the settled patch with only the mutation and read starting: %+v, starts=%d, err=%v", row.State.Outcome, worker.starts.Load(), err)
	}
}
