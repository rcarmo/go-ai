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

func TestCodemode120ImageLabelsTriggerCompleteSpill(t *testing.T) {
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
	backends(t, func(t *testing.T, b backend) {
		writer := &outputWriter120{}
		registry := NewRegistry()
		def := taskDefinition(t, "label-spill-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			code := "// @options: {\"max_output_tokens\":1}\nimage({type:'image',mimeType:'image/png',data:'" + data + "'});text('ok');"
			result, err := r.RunCodemode(ctx, "run", code, engine)
			if err != nil {
				return err
			}
			if result.Error != "" || result.FullOutputPath != "/owned/output.txt" || len(result.Images) != 1 || codemodeTextUnits(result.Output) > 4 {
				t.Error(result)
			}
			if _, err = r.RunCodemode(ctx, "run", code, engine); err != nil {
				return err
			}
			writer.mu.Lock()
			count := writer.count
			full := writer.writes["output.txt"]
			writer.mu.Unlock()
			if count != 2 || !strings.Contains(full, "[Image saved to /owned/image-1.png") || !strings.HasSuffix(full, "\nok") {
				t.Error("incomplete labelled spill", count, full)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return writer, nil }})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
