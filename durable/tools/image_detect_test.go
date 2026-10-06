package tools

import (
	"context"
	"encoding/binary"
	"github.com/rcarmo/go-ai/durable"
	"os"
	"path/filepath"
	"testing"
)

func TestPinnedImageDetectionBMPWebPAPNGAndNativeReadBoundary(t *testing.T) {
	bmp := make([]byte, 58)
	copy(bmp, "BM")
	binary.LittleEndian.PutUint32(bmp[2:], 58)
	binary.LittleEndian.PutUint32(bmp[10:], 54)
	binary.LittleEndian.PutUint32(bmp[14:], 40)
	binary.LittleEndian.PutUint16(bmp[26:], 1)
	binary.LittleEndian.PutUint16(bmp[28:], 24)
	png := append([]byte{137, 80, 78, 71, 13, 10, 26, 10, 0, 0, 0, 13}, []byte("IHDR")...)
	png = append(png, make([]byte, 17)...)
	apng := append(append([]byte{}, png...), []byte{0, 0, 0, 8, 'a', 'c', 'T', 'L'}...)
	for _, c := range []struct {
		data []byte
		mime string
	}{{bmp, "image/bmp"}, {[]byte("RIFF1234WEBP"), "image/webp"}, {png, "image/png"}, {apng, ""}, {[]byte("GIF89a"), "image/gif"}, {[]byte{255, 216, 255, 0xf7}, ""}, {[]byte{255, 216, 255}, "image/jpeg"}, {[]byte("text"), ""}} {
		if got := detectImageMime(c.data); got != c.mime {
			t.Fatal(c.data, got, c.mime)
		}
	}
	invalid := append([]byte{}, bmp...)
	binary.LittleEndian.PutUint16(invalid[26:], 2)
	if detectImageMime(invalid) != "" {
		t.Fatal("invalid BMP planes")
	}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "image.bmp"), bmp, 0600); err != nil {
		t.Fatal(err)
	}
	result, err := Read(LocalEnv(directory)).Execute(context.Background(), durable.JSON{"path": "image.bmp"}, nil)
	if err != nil || !result.IsError || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "unsupported_image" {
		t.Fatal(result, err)
	}
	fs := &memoryToolFS{files: map[string][]byte{"/virtual/image": []byte("RIFF1234WEBP")}, id: "images"}
	result, err = Read(fs).Execute(context.Background(), durable.JSON{"path": "image"}, nil)
	if err != nil || !result.IsError || result.Diagnostics[0].Message != "image is an image (image/webp); reading images is not supported" {
		t.Fatal(result, err)
	}
}
