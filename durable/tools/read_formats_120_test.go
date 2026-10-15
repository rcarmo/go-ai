package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

// Default read is a no-processor path: valid inline formats retain their
// original bytes, and budgets are checked against those bytes and dimensions.
func TestRead120InlineFormatsAndExactBudgets(t *testing.T) {
	raster := image.NewNRGBA(image.Rect(0, 0, 8, 6))
	for _, format := range []string{"png", "jpeg", "gif", "webp"} {
		t.Run(format, func(t *testing.T) {
			var buffer bytes.Buffer
			var err error
			switch format {
			case "png":
				err = png.Encode(&buffer, raster)
			case "jpeg":
				err = jpeg.Encode(&buffer, raster, nil)
			case "gif":
				err = gif.Encode(&buffer, raster, nil)
			case "webp":
				var data []byte
				data, err = os.ReadFile("testdata/gopher-doc-lossless.webp")
				buffer.Write(data)
			}
			if err != nil {
				t.Fatal(err)
			}
			data := buffer.Bytes()
			config, _, err := image.DecodeConfig(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			limits := ImageLimits{MaxWidth: config.Width, MaxHeight: config.Height, MaxBase64: base64.StdEncoding.EncodedLen(len(data))}
			block, details, err := inlineImageBytes(context.Background(), data, limits)
			if err != nil || block.MimeType != "image/"+format || details["width"] != config.Width || details["height"] != config.Height {
				t.Fatal(block.MimeType, details, err)
			}
			decoded, err := base64.StdEncoding.DecodeString(block.Data)
			if err != nil || !bytes.Equal(decoded, data) {
				t.Fatal("inline bytes changed", err)
			}
			for _, field := range []string{"width", "height", "bytes"} {
				bad := limits
				switch field {
				case "width":
					bad.MaxWidth--
				case "height":
					bad.MaxHeight--
				case "bytes":
					bad.MaxBase64--
				}
				if _, _, err = inlineImageBytes(context.Background(), data, bad); err == nil {
					t.Fatal("budget bypass", field)
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if _, _, err = inlineImageBytes(ctx, data, limits); !errors.Is(err, context.Canceled) {
				t.Fatal("cancelled inline image", err)
			}
		})
	}
}

func TestRead120InlineRejectsTruncatedImageHeaders(t *testing.T) {
	for _, data := range [][]byte{
		{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0, 0, 0, 8, 0, 0, 0, 6},
		[]byte("GIF89a\x08\x00\x06\x00"),
		[]byte("RIFF1234WEBP"),
		{0xff, 0xd8, 0xff},
	} {
		if _, _, err := inlineImageBytes(context.Background(), data, ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 1024}); err == nil {
			t.Fatal("truncated image header accepted")
		}
	}
}
