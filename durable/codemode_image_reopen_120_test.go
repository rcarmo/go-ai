package durable

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"sync/atomic"
	"testing"
)

func TestCodemode120QuickJSFailedImageOutputReopensWithoutEffectRepeat(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	raster := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	raster.SetNRGBA(0, 0, color.NRGBA{B: 255, A: 255})
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, raster); err != nil {
		t.Fatal(err)
	}
	data := base64.StdEncoding.EncodeToString(encoded.Bytes())
	backends(t, func(t *testing.T, b backend) {
		sandbox := &restartQuickJS120{sandbox: engine, ready: make(chan struct{})}
		var effects atomic.Int64
		registry := NewRegistry()
		leaf := wrapRegistration("image_leaf")
		leaf.Execute = func(context.Context, JSON, *ToolAPI) (ToolResult, error) {
			effects.Add(1)
			return ToolResult{Content: "ok"}, nil
		}
		if err = registry.Register(leaf); err != nil {
			t.Fatal(err)
		}
		def := taskDefinition(t, "failed-image-reopen-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			code := `await tools.image_leaf({});image({type:'image',mimeType:'image/png',data:'` + data + `'});store('late',true);throw Error('failed');`
			result, err := r.RunCodemode(ctx, "run", code, sandbox)
			if err != nil {
				return err
			}
			if result.Error == "" || len(result.Images) != 1 {
				t.Errorf("failed image lost %+v", result)
			}
			raw, err := base64.StdEncoding.DecodeString(result.Images[0].Data)
			if err != nil {
				return err
			}
			decoded, err := png.Decode(bytes.NewReader(raw))
			if err != nil {
				return err
			}
			_, _, blue, _ := decoded.At(0, 0).RGBA()
			if decoded.Bounds().Dx() != 1 || blue != 65535 {
				t.Error("replay pixel", decoded.Bounds(), blue)
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
				if len(object["values"].(map[string]any)) != 0 {
					t.Error("failed store adopted")
				}
				return taskDone(nil), nil
			})
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if err = h.Resume(bg); err != nil {
			t.Fatal(err)
		}
		awaitTaskSignal(t, sandbox.ready)
		if err = h.Close(bg); err != nil {
			t.Fatal(err)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || effects.Load() != 1 || sandbox.starts.Load() != 2 {
			t.Fatalf("%+v %+v effects=%d starts=%d", record.State.Outcome, record.State.Outcome.Error, effects.Load(), sandbox.starts.Load())
		}
	})
}
