package durable

import (
	"context"
	"encoding/base64"
	"strings"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

type imageChunkSandbox120 struct {
	data   string
	starts *atomic.Int64
}

func (s imageChunkSandbox120) Start(context.Context, string, CodemodeStart) (CodemodeExecution, error) {
	s.starts.Add(1)
	return &imageChunkExecution120{data: s.data}, nil
}

type imageChunkExecution120 struct{ data string }

func (e *imageChunkExecution120) Next(context.Context) (CodemodeEvent, error) {
	return CodemodeEvent{Result: &CodemodeResult{Images: []goai.ImageContent{{Type: "image", MimeType: "image/png", Data: e.data}}}}, nil
}
func (*imageChunkExecution120) Deliver(context.Context, CodemodeDelivery) error { return nil }
func (*imageChunkExecution120) Close() error                                    { return nil }

func TestCodemode120LargeImageChunkResultReplay(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		data := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("large-image", 40000)))
		var starts atomic.Int64
		registry := NewRegistry()
		task := taskDefinition(t, "image-chunks-120", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			for i := 0; i < 2; i++ {
				result, err := r.RunCodemode(ctx, "run", "image", imageChunkSandbox120{data, &starts})
				if err != nil {
					return err
				}
				if len(result.Images) != 1 || result.Images[0].Data != data {
					t.Error("large image not reconstructed")
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(task); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, task, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatal(record.State.Outcome)
		}
		if starts.Load() != 1 {
			t.Fatal("settled VM reran", starts.Load())
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		chunks := 0
		for _, doc := range state.Documents {
			if doc.Kind == codemodeImageChunkKind {
				chunks++
				if len(doc.Value["data"].(string)) > DefaultLimits().MaxStringBytes {
					t.Fatal("ordinary limits exceeded")
				}
			}
		}
		if chunks < 2 {
			t.Fatal("large image not chunked", chunks)
		}
		if err := h.Close(bg); err != nil {
			t.Fatal(err)
		}
		store := reopenStoreAfterHarnessClose(t, b.store)
		defer store.Close(bg)
		if _, err := store.Snapshot(bg); err != nil {
			t.Fatal("chunk state did not reopen", err)
		}
	})
}
