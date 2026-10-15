package durable

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func crashImageTask120(t *testing.T, registry *Registry) *TaskDefinition {
	t.Helper()
	data := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("image", 100000)))
	def := taskDefinition(t, "image-process-recovery", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		// The VM hasn't settled when the child stops. Retry must reuse the prefix
		// but execute the sandbox again; only confirmed outcomes suppress execution.
		var starts atomic.Int64
		result, err := r.RunCodemode(ctx, "run", "image", imageChunkSandbox120{data, &starts})
		if err != nil {
			return err
		}
		if len(result.Images) != 1 || result.Images[0].Data != data {
			return errors.New("recovered image differs")
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err := registry.RegisterTask(def); err != nil {
		t.Fatal(err)
	}
	return def
}

func TestCodemode120ImageProcessHelper(t *testing.T) {
	dir := os.Getenv("GOAI_IMAGE_CRASH_DIR")
	if dir == "" {
		return
	}
	storage, err := OpenJournal(dir, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	def := crashImageTask120(t, registry)
	h := openHarness(t, storage, Options{Registry: registry})
	core := h.session.core
	<-core.line
	original := core.appendFrame
	core.leave()
	installTaskAppend(t, h, core, func(kind byte, seq, high uint64, payload []byte) error {
		if err := original(kind, seq, high, payload); err != nil {
			return err
		}
		if kind == 2 {
			var record commitRecord
			if err := json.Unmarshal(payload, &record); err != nil {
				return err
			}
			for _, write := range record.Writes {
				if write.Op == "put-document" && write.Document != nil && write.Document.Kind == codemodeImageChunkKind {
					// Abrupt exit deliberately bypasses Session/storage close and all defers.
					os.Exit(73)
				}
			}
		}
		return nil
	})
	createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
	if err = h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Second)
	t.Fatal("no committed image chunk")
}

func TestCodemode120ImageChunkProcessExitRecovery(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "journal")
	ctx, cancel := context.WithTimeout(bg, 20*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCodemode120ImageProcessHelper$")
	cmd.Env = append(os.Environ(), "GOAI_IMAGE_CRASH_DIR="+dir)
	output, err := cmd.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 73 {
		t.Fatal("helper did not stop at commit boundary", err, string(output))
	}
	storage, err := OpenJournal(dir, JournalOptions{})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := storage.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	chunks := 0
	for _, doc := range snapshot.Documents {
		if doc.Kind == codemodeImageChunkKind {
			chunks++
		}
	}
	if chunks != 1 {
		t.Fatal("abrupt exit lost committed prefix", chunks)
	}
	var task ID
	for id, raw := range snapshot.Tasks {
		if raw.Kind == "image-process-recovery" {
			task = id
		}
	}
	if task == 0 {
		t.Fatal("task missing after process exit")
	}
	registry := NewRegistry()
	crashImageTask120(t, registry)
	h := openHarness(t, storage, Options{Registry: registry})
	if err = h.Resume(bg); err != nil {
		t.Fatal(err)
	}
	record := waitPublicTask(t, h, task)
	if record.State.Outcome.Status != "completed" {
		t.Fatal(record.State.Outcome)
	}
	snapshot, err = h.Snapshot(bg)
	if err != nil {
		t.Fatal(err)
	}
	chunks = 0
	for _, doc := range snapshot.Documents {
		if doc.Kind == codemodeImageChunkKind {
			chunks++
		}
	}
	dataBytes := base64.StdEncoding.EncodedLen(500000)
	size := DefaultLimits().MaxStringBytes / 2
	if chunks != (dataBytes+size-1)/size {
		t.Fatal("process retry duplicated chunks", chunks)
	}
}
