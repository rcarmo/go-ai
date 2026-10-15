package durable

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestCodemode120PartialImageChunksReuseAfterCancelledAdmission(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		var mu sync.Mutex
		var cancelFirst context.CancelFunc
		writes := 0
		registry := NewRegistry()
		data := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("image", 100000)))
		def := taskDefinition(t, "partial-image-chunks", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			attempt, cancel := context.WithCancel(ctx)
			defer cancel()
			mu.Lock()
			cancelFirst = cancel
			mu.Unlock()
			image := []goai.ImageContent{{Type: "image", MimeType: "image/png", Data: data}}
			if _, err := r.persistCodemodeImages(attempt, "partial", image, r.harness.session.limits); !errors.Is(err, context.Canceled) {
				t.Errorf("partial attempt: %v", err)
			}
			refs, err := r.persistCodemodeImages(ctx, "partial", image, r.harness.session.limits)
			if err != nil {
				return err
			}
			return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
				restored, err := readCodemodeImages(tx.state, r.taskID, "partial", refs, tx.limits)
				if err != nil {
					return nil, err
				}
				if len(restored) != 1 || restored[0].Data != data {
					t.Error("partial image not restored")
				}
				mu.Lock()
				count := writes
				mu.Unlock()
				if count != len(refs[0].Chunks) {
					t.Error("partial retry duplicated chunks", count, len(refs[0].Chunks))
				}
				return taskDone(nil), nil
			})
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		core := h.session.core
		<-core.line
		original := core.appendFrame
		core.leave()
		installTaskAppend(t, h, core, func(kind byte, seq, high uint64, payload []byte) error {
			if original != nil {
				if err := original(kind, seq, high, payload); err != nil {
					return err
				}
			}
			if kind == 2 {
				var record commitRecord
				if err := json.Unmarshal(payload, &record); err != nil {
					return err
				}
				for _, write := range record.Writes {
					if write.Document != nil && write.Document.Kind == codemodeImageChunkKind && write.Op == "put-document" {
						mu.Lock()
						writes++
						cancel := cancelFirst
						cancelFirst = nil
						mu.Unlock()
						if cancel != nil {
							cancel()
						}
					}
				}
			}
			return nil
		})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
	})
}
