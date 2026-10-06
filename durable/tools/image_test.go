package tools

import (
	"context"
	"encoding/base64"
	"github.com/rcarmo/go-ai/durable"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestReadImageOwnsValidInlinePNGAndRejectsBroken(t *testing.T) {
	directory := t.TempDir()
	env := LocalEnv(directory)
	path := filepath.Join(directory, "image.png")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	raster := image.NewRGBA(image.Rect(0, 0, 2, 3))
	raster.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(file, raster); err != nil {
		t.Fatal(err)
	}
	file.Close()
	result, err := ReadWithImages(env).Execute(context.Background(), durable.JSON{"path": "image.png"}, nil)
	if err != nil || len(result.Blocks) != 1 || result.Blocks[0].Type != "image" || result.Blocks[0].MimeType != "image/png" || result.Details["width"] != 2 || result.Details["height"] != 3 {
		t.Fatal(result, err)
	}
	data, err := base64.StdEncoding.DecodeString(result.Blocks[0].Data)
	if err != nil || len(data) == 0 {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data[:16], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadWithImages(env).Execute(context.Background(), durable.JSON{"path": "image.png"}, nil); err == nil {
		t.Fatal("invalid image accepted")
	}
}
