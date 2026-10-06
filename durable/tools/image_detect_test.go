package tools

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
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

func pngChunk104(kind string, length int) []byte {
	data := make([]byte, length+12)
	binary.BigEndian.PutUint32(data, uint32(length))
	copy(data[4:], kind)
	return data
}
func pngChunks104(marker string, padding int) []byte {
	data := append([]byte{}, []byte{137, 80, 78, 71, 13, 10, 26, 10}...)
	data = append(data, pngChunk104("IHDR", 13)...)
	data = append(data, pngChunk104("tEXt", padding)...)
	return append(data, pngChunk104(marker, 8)...)
}
func TestRead104PositionalImageChunkHeadersBeyondProbe(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	env := &countedEnv104{Env: LocalEnv(dir)}
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"late-animation", pngChunks104("acTL", 70000)},
		{"block-edge", pngChunks104("acTL", 65534)},
		{"still-image", pngChunks104("IDAT", 70000)},
		{"IDAT-before-animation", append(pngChunks104("IDAT", 70000), pngChunk104("acTL", 8)...)},
		{"invalid-length", append(pngChunks104("tEXt", 0), []byte{255, 255, 255, 255, 't', 'E', 'X', 't'}...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(dir, tc.name), tc.data, 0600); err != nil {
				t.Fatal(err)
			}
			args := durable.JSON{"path": tc.name}
			got, err := Read(env).Execute(ctx, args, nil)
			if err != nil {
				t.Fatal(err)
			}
			want, wantErr := Read(legacyRead104{env.Env}).Execute(ctx, args, nil)
			if wantErr != nil {
				t.Fatal(wantErr)
			}
			if got.IsError != want.IsError || !bytes.Equal([]byte(got.Content), []byte(want.Content)) {
				t.Fatal("positional/whole-file MIME differs", got.IsError, want.IsError)
			}
			if env.opened.largest > 64<<10 || env.opened.bytes > 3*(64<<10)+512 {
				t.Fatal("unbounded PNG reads", env.opened.bytes, env.opened.largest)
			}
		})
	}
}

type failingImageReader104 struct {
	durable.BinaryReader
	at int64
}

func (r failingImageReader104) Read(ctx context.Context, at int64, n int) ([]byte, error) {
	if at >= r.at {
		return nil, errors.New("injected PNG chunk read failure")
	}
	return r.BinaryReader.Read(ctx, at, n)
}

func TestRead104ImageChunkReadFailurePropagates(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	env := LocalEnv(dir)
	data := pngChunks104("acTL", 70000)
	if err := os.WriteFile(filepath.Join(dir, "file"), data, 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := env.OpenBinaryReader(ctx, "file", false)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	_, err = detectImageMimeOf(ctx, failingImageReader104{BinaryReader: reader, at: 8}, int64(len(data)), data[:512])
	if err == nil || err.Error() != "injected PNG chunk read failure" {
		t.Fatal("image read error swallowed", err)
	}
}
