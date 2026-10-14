package durable

import (
	"bytes"
	"context"
	"encoding/base64"
	goai "github.com/rcarmo/go-ai"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestCodemode120QuickJSImageOutputPersistedAndDetached(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	raster := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	raster.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	raster.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	var encoded bytes.Buffer
	if err = png.Encode(&encoded, raster); err != nil {
		t.Fatal(err)
	}
	data := base64.StdEncoding.EncodeToString(encoded.Bytes())
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		def := taskDefinition(t, "image-output-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			code := "// @options: {\"max_output_tokens\":0}\nimage({type:'image',mimeType:'image/png',data:'" + data + "'});text('discarded');"
			first, err := r.RunCodemode(ctx, "run", code, engine)
			if err != nil {
				return err
			}
			if first.Error != "" || first.Output != "" || len(first.Images) != 1 {
				t.Error(first)
			}
			first.Images[0].Data = "mutated"
			second, err := r.RunCodemode(ctx, "run", code, engine)
			if err != nil {
				return err
			}
			if len(second.Images) != 1 {
				t.Fatal(second)
			}
			raw, err := base64.StdEncoding.DecodeString(second.Images[0].Data)
			if err != nil {
				return err
			}
			decoded, err := png.Decode(bytes.NewReader(raw))
			if err != nil {
				return err
			}
			if decoded.Bounds().Dx() != 2 || decoded.Bounds().Dy() != 1 {
				t.Error(decoded.Bounds())
			}
			red, _, _, _ := decoded.At(0, 0).RGBA()
			if red != 65535 {
				t.Error("pixel changed", red)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
	})
}
func TestCodemode120ImageOutputValidation(t *testing.T) {
	for _, image := range []goai.ImageContent{{Type: "image", Data: "not base64", MimeType: "image/png"}, {Type: "image", Data: "aGVsbG8=", MimeType: "text/plain"}, {Type: "text", Data: "aGVsbG8=", MimeType: "image/png"}} {
		if _, err := ownCodemodeResult(CodemodeResult{Images: []goai.ImageContent{image}}, DefaultLimits()); err == nil {
			t.Fatal("invalid image admitted", image)
		}
	}
}
