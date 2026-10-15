package durable

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120ImageChunkAppendFailureReopen(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		data := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("image", 100000)))
		var starts atomic.Int64
		registry := NewRegistry()
		def := taskDefinition(t, "image-poison-reopen", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, err := r.RunCodemode(ctx, "run", "image", imageChunkSandbox120{data, &starts})
			if err != nil {
				return err
			}
			if len(result.Images) != 1 || result.Images[0].Data != data {
				t.Error("recovered image corrupted")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		core := h.session.core
		<-core.line
		original := core.appendFrame
		core.leave()
		chunks := 0
		failure := errors.New("injected image append failure")
		installTaskAppend(t, h, core, func(kind byte, seq, high uint64, payload []byte) error {
			if kind == 2 {
				var record commitRecord
				if err := json.Unmarshal(payload, &record); err != nil {
					return err
				}
				for _, write := range record.Writes {
					if write.Op == "put-document" && write.Document != nil && write.Document.Kind == codemodeImageChunkKind {
						chunks++
						if chunks == 2 {
							return failure
						}
					}
				}
			}
			if original != nil {
				return original(kind, seq, high, payload)
			}
			return nil
		})
		handle := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err := h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(bg, 10*time.Second)
		defer cancel()
		end, err := h.session.Closed(ctx)
		if err != nil || end.Reason != "failed" || !errors.Is(end.Error, ErrPoisoned) {
			t.Fatal(end, err)
		}
		if err = h.Close(ctx); err != nil {
			t.Fatal(err)
		}
		store := reopenStoreAfterHarnessClose(t, b.store)
		snapshot, err := store.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		retained := 0
		for _, doc := range snapshot.Documents {
			if doc.Kind == codemodeImageChunkKind {
				retained++
			}
		}
		if retained != 1 {
			t.Fatal("partial durable chunk set", retained)
		}
		h2 := openHarness(t, store, Options{Registry: registry})
		if err = h2.Resume(bg); err != nil {
			t.Fatal(err)
		}
		record := waitPublicTask(t, h2, handle)
		if record.State.Outcome.Status != "completed" || starts.Load() != 2 {
			t.Fatal(record.State.Outcome, starts.Load())
		}
		state, err := h2.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, doc := range state.Documents {
			if doc.Kind == codemodeImageChunkKind {
				count++
			}
		}
		expected := (len(data) + (DefaultLimits().MaxStringBytes / 2) - 1) / (DefaultLimits().MaxStringBytes / 2)
		if count != expected {
			t.Fatal("reopen duplicated chunk set", count, expected)
		}
	})
}
