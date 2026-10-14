package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/rcarmo/go-ai/durable"
)

func TestRead120PreparedImageUsesOwnedLocalPathAndLimits(t *testing.T) {
	root := t.TempDir()
	src := image.NewNRGBA(image.Rect(0, 0, 20, 10))
	var data bytes.Buffer
	if e := png.Encode(&data, src); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, "large.png"), data.Bytes(), 0600); e != nil {
		t.Fatal(e)
	}
	env := LocalEnv(root)
	reg := ReadWithPreparedImages(env, ImageLimits{MaxWidth: 4, MaxHeight: 4, MaxBase64: 1024})
	result, e := reg.Execute(context.Background(), durable.JSON{"path": "large.png"}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if len(result.Blocks) != 1 || result.Details["width"] != 4 || result.Details["height"] != 2 || result.Details["originalWidth"] != 20 {
		t.Fatal(result)
	}
	encoded, e := base64.StdEncoding.DecodeString(result.Blocks[0].Data)
	if e != nil {
		t.Fatal(e)
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(encoded))
	if e != nil || cfg.Width != 4 || cfg.Height != 2 {
		t.Fatal(cfg, e)
	}
}
