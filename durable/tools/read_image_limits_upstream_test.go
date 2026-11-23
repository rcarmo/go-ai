package tools

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"

	"github.com/rcarmo/go-ai/durable"
)

// Source: pi v1.6.0 packages/durable/test/tools-images.test.ts:120-145.
func TestSourceV160InlineReadImageSafety(t *testing.T) {
	pngAt := func(w, h int, compression png.CompressionLevel) []byte {
		pixels := image.NewNRGBA(image.Rect(0, 0, w, h))
		for i := 3; i < len(pixels.Pix); i += 4 {
			pixels.Pix[i] = 255
		}
		var dst bytes.Buffer
		encoder := png.Encoder{CompressionLevel: compression}
		if err := encoder.Encode(&dst, pixels); err != nil {
			t.Fatal(err)
		}
		return dst.Bytes()
	}
	large := pngAt(1100, 1100, png.NoCompression)
	if len(large) >= MaxImageBytes || base64.StdEncoding.EncodedLen(len(large)) <= 9<<19 {
		t.Fatalf("invalid fixture: raw bytes=%d, base64 bytes=%d", len(large), base64.StdEncoding.EncodedLen(len(large)))
	}
	var tall bytes.Buffer
	gifPixels := image.NewPaletted(image.Rect(0, 0, 1, 3000), color.Palette{color.Black, color.White})
	if err := gif.Encode(&tall, gifPixels, nil); err != nil {
		t.Fatal(err)
	}
	// 4x4, 24-bit BMP, whose rows occupy 4*3 bytes without padding.
	bmp := make([]byte, 54+4*4*3)
	copy(bmp, "BM")
	binary.LittleEndian.PutUint32(bmp[2:], uint32(len(bmp)))
	binary.LittleEndian.PutUint32(bmp[10:], 54)
	binary.LittleEndian.PutUint32(bmp[14:], 40)
	binary.LittleEndian.PutUint32(bmp[18:], 4)
	binary.LittleEndian.PutUint32(bmp[22:], 4)
	binary.LittleEndian.PutUint16(bmp[26:], 1)
	binary.LittleEndian.PutUint16(bmp[28:], 24)
	for _, tc := range []struct {
		name    string
		data    []byte
		exact   string
		pattern string
	}{
		{"big.png", large, "", `^big\.png is an image \(image/png\) of .*, too large to send`},
		{"wide.png", pngAt(2001, 2, png.DefaultCompression), "wide.png is an image (image/png) of 2001x2, larger than 2000x2000, and no image processor is configured to shrink it", ""},
		{"tall.gif", tall.Bytes(), "", `^tall\.gif is an image \(image/gif\) of 1x3000, larger than 2000x2000`},
		{"a.bmp", bmp, "a.bmp is an image (image/bmp) that needs converting, and no image processor is configured", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, tc.name), tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			local := LocalEnv(root)
			counted := &countedEnv104{Env: local}
			args := durable.JSON{"path": tc.name}
			got, err := Read(counted).Execute(t.Context(), args, nil)
			if err != nil || !got.IsError || len(got.Blocks) != 0 || len(got.Diagnostics) != 1 || got.Diagnostics[0].Severity != "error" || got.Diagnostics[0].Code != "unsupported_image" {
				t.Fatalf("result=%+v err=%v; want one error diagnostic and no blocks", got, err)
			}
			message := got.Diagnostics[0].Message
			if tc.exact != "" && message != tc.exact || tc.pattern != "" && !regexp.MustCompile(tc.pattern).MatchString(message) {
				t.Errorf("diagnostic = %q; want exact %q or prefix pattern %q", message, tc.exact, tc.pattern)
			}
			if tc.name == "big.png" && (counted.opened == nil || counted.opened.largest > 64<<10 || counted.opened.bytes > (64<<10)+512) {
				t.Errorf("oversized file read past its header and chunk walk: %+v", counted.opened)
			}
			// The same tool backed by the ordinary local environment must not
			// bypass the guarded behavior of its positional reader.
			localResult, localErr := Read(local).Execute(t.Context(), args, nil)
			if localErr != nil || !reflect.DeepEqual(localResult, got) {
				t.Errorf("local result=%+v err=%v; want same result as positional wrapper", localResult, localErr)
			}
		})
	}
}
