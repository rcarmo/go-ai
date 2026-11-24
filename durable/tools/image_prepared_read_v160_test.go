package tools

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/rcarmo/go-ai/durable"
)

// Source: pi v1.6.0 tools-images.test.ts:198–212 and 254–259.
func TestSourceV160PreparedImageReadDiagnostics(t *testing.T) {
	pngAt := func(t *testing.T, w, h int) []byte {
		t.Helper()
		var data bytes.Buffer
		if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, w, h))); err != nil {
			t.Fatal(err)
		}
		return data.Bytes()
	}
	read := func(t *testing.T, name string, data []byte) durable.ToolResult {
		t.Helper()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, name), data, 0600); err != nil {
			t.Fatal(err)
		}
		got, err := ReadWithImages(LocalEnv(root)).Execute(t.Context(), durable.JSON{"path": name}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	t.Run("unchanged", func(t *testing.T) {
		data := pngAt(t, 8, 6)
		got := read(t, "a.png", data)
		if got.IsError || len(got.Blocks) != 1 || got.Blocks[0].Type != "image" || got.Blocks[0].MimeType != "image/png" || got.Blocks[0].Data != base64.StdEncoding.EncodeToString(data) {
			t.Fatalf("unchanged PNG: %+v", got)
		}
		var gifData bytes.Buffer
		if err := gif.Encode(&gifData, image.NewNRGBA(image.Rect(0, 0, 8, 6)), nil); err != nil {
			t.Fatal(err)
		}
		got = read(t, "a.gif", gifData.Bytes())
		if got.IsError || len(got.Blocks) != 1 || got.Blocks[0].Type != "image" || got.Blocks[0].MimeType != "image/gif" {
			t.Fatalf("unchanged GIF: %+v", got)
		}
	})
	t.Run("resized", func(t *testing.T) {
		got := read(t, "wide.png", pngAt(t, 4000, 1000))
		if got.IsError || len(got.Blocks) != 1 || got.Blocks[0].Type != "image" {
			t.Fatalf("resized image missing: %+v", got)
		}
		block := got.Blocks[0]
		decoded, err := base64.StdEncoding.DecodeString(block.Data)
		if err != nil {
			t.Fatal(err)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(decoded))
		if err != nil || config.Width != 2000 || config.Height != 500 {
			t.Fatalf("decoded image = %dx%d, err=%v; want 2000x500", config.Width, config.Height, err)
		}
		want := fmt.Sprintf("Read image file [%s]. Resized from 4000x1000 to 2000x500. Multiply coordinates by 2.00 to map them to the original.", block.MimeType)
		if len(got.Diagnostics) != 1 || got.Diagnostics[0].Message != want {
			t.Fatalf("resize diagnostic=%+v; want %q", got.Diagnostics, want)
		}
	})
	t.Run("undecodable", func(t *testing.T) {
		got := read(t, "broken.png", pngAt(t, 4, 4)[:40])
		if !got.IsError || len(got.Blocks) != 0 {
			t.Fatalf("invalid image was not refused: %+v", got)
		}
		const want = "broken.png is an image (image/png) that cannot be prepared for the model"
		if len(got.Diagnostics) != 1 || got.Diagnostics[0].Message != want {
			t.Fatalf("decode diagnostic=%+v; want %q", got.Diagnostics, want)
		}
	})
}

// Source: pi v1.6.0 tools.test.ts:206–208 and 311–320.
func TestSourceV160ReadImageByContent(t *testing.T) {
	for _, signature := range []string{"GIF87a", "GIF89a"} {
		t.Run(signature, func(t *testing.T) {
			if got := detectImageMime([]byte(signature)); got != "image/gif" {
				t.Fatalf("complete %s signature = %q; want image/gif", signature, got)
			}
		})
	}
	t.Run("png-in-text-file", func(t *testing.T) {
		const data = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR4nGNgYGD4DwABBAEAX+XDSwAAAABJRU5ErkJggg=="
		decoded, err := base64.StdEncoding.DecodeString(data)
		if err != nil {
			t.Fatal(err)
		}
		path := t.TempDir()
		if err := os.WriteFile(filepath.Join(path, "image.txt"), decoded, 0600); err != nil {
			t.Fatal(err)
		}
		got, err := Read(LocalEnv(path)).Execute(t.Context(), durable.JSON{"path": "image.txt"}, nil)
		if err != nil || got.IsError || len(got.Blocks) != 1 || got.Blocks[0].Type != "image" || got.Blocks[0].MimeType != "image/png" || got.Blocks[0].Data != data {
			t.Fatalf("PNG detected by content: %+v, err=%v", got, err)
		}
		const want = "Read image file [image/png]."
		if len(got.Diagnostics) != 1 || got.Diagnostics[0].Message != want {
			t.Fatalf("inline diagnostic=%+v; want %q", got.Diagnostics, want)
		}
	})
}
