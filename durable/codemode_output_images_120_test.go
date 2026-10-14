package durable

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"strings"
	"testing"
)

func TestCodemode120OutputImageWriterDedupFailureAndReuse(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := base64.StdEncoding.EncodeToString(encoded.Bytes())
	for _, fail := range []bool{false, true} {
		backends(t, func(t *testing.T, b backend) {
			writer := &outputWriter120{fail: fail}
			registry := NewRegistry()
			def := taskDefinition(t, "image-writer-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				code := `image({type:'image',mimeType:'image/png',data:'` + data + `'});image({type:'image',mimeType:'image/png',data:'` + data + `'});store('valid',true);`
				result, err := r.RunCodemode(ctx, "run", code, engine)
				if err != nil {
					return err
				}
				if result.Error != "" || len(result.Images) != 2 || strings.Contains(result.Output, "private") {
					t.Error(result)
				}
				if fail && !strings.Contains(result.Output, "could not be saved") {
					t.Error("failure label missing", result)
				}
				if !fail && strings.Count(result.Output, "/owned/image-1.png") != 2 {
					t.Error("duplicate label", result.Output)
				}
				if _, err = r.RunCodemode(ctx, "run", code, engine); err != nil {
					return err
				}
				writer.mu.Lock()
				count := writer.count
				raw := writer.writes["image-1.png"]
				writer.mu.Unlock()
				if count != 1 {
					t.Error("duplicate/replay write", count)
				}
				if !fail {
					decoded, err := png.Decode(strings.NewReader(raw))
					if err != nil {
						return err
					}
					if decoded.Bounds().Dx() != 1 {
						t.Error(decoded.Bounds())
					}
				}
				return r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
					store, err := tx.AcquireDocument(codemodeStoreDefinition(), 1, nil, nil)
					if err != nil {
						return nil, err
					}
					object, err := store.Get()
					if err != nil {
						return nil, err
					}
					if object["values"].(map[string]any)["valid"] != true {
						t.Error("writer failure discarded successful script")
					}
					return taskDone(nil), nil
				})
			})
			if _, err := registry.RegisterTask(def); err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, b.store, Options{Registry: registry, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return writer, nil }})
			record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
			if record.State.Outcome.Status != "completed" {
				t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
			}
		})
	}
}
func TestCodemode120FullTextBudget(t *testing.T) {
	for _, text := range []string{strings.Repeat("a", MaxCodemodeFullOutputBytes+1), string([]byte{255})} {
		if validateCodemodeFullText(text) == nil {
			t.Fatal("invalid full output accepted")
		}
	}
	if err := validateCodemodeFullText("😀"); err != nil {
		t.Fatal(err)
	}
}
