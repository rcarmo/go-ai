package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
)

const MaxImageBytes = 8 << 20
const MaxImagePixels = 40_000_000

// LoadImage validates raster dimensions before exposing a bounded inline image.
// PNG/JPEG/GIF use standard-library decoders. Unsupported formats reject.
func (e *Env) LoadImage(ctx context.Context, path string) (goai.ContentBlock, durable.JSON, error) {
	if err := ctx.Err(); err != nil {
		return goai.ContentBlock{}, nil, err
	}
	absolute, err := e.resolve(path)
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	file, err := os.Open(absolute)
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, MaxImageBytes+1))
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	if len(data) > MaxImageBytes {
		return goai.ContentBlock{}, nil, errors.New("image exceeds byte limit")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return goai.ContentBlock{}, nil, err
	}
	if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > MaxImagePixels {
		return goai.ContentBlock{}, nil, errors.New("image exceeds pixel limit")
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return goai.ContentBlock{}, nil, err
	}
	mime := "image/" + format
	if format == "jpg" {
		mime = "image/jpeg"
	}
	return goai.ContentBlock{Type: "image", MimeType: mime, Data: base64.StdEncoding.EncodeToString(data)}, durable.JSON{"path": path, "width": config.Width, "height": config.Height, "mimeType": mime}, nil
}
func isRasterImage(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	prefix := make([]byte, 16)
	n, _ := file.Read(prefix)
	prefix = prefix[:n]
	return bytes.HasPrefix(prefix, []byte{137, 80, 78, 71, 13, 10, 26, 10}) || bytes.HasPrefix(prefix, []byte{255, 216, 255}) || bytes.HasPrefix(prefix, []byte("GIF87a")) || bytes.HasPrefix(prefix, []byte("GIF89a"))
}
