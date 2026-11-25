package tools_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"github.com/rcarmo/go-ai/durable/tools"
)

// The same pinned Photon encode(8, 6, "png") as tools-images.test.ts:174,189.
const changingImagePNG = "iVBORw0KGgoAAAANSUhEUgAAAAgAAAAGCAYAAAD+Bd/7AAAAUElEQVR4Ae3AA6AkWZbG8f937o3IzKdyS2Oubdu2bdu2bdu2bWmMnpZKr54yMyLu+Xa3anqmhztr1a9ibPGCUC1eGCovHJUXjsoLR+WF4x8B9TAEF4Sg4YsAAAAASUVORK5CYII="

func TestSourceV160ChangingImageRead(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(changingImagePNG)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, path     string
		appendBytes    int
		appends        int
		repeatMutation bool
	}{
		{"growing", "growing.png", 16, 1, false}, // L173–186
		{"busy", "busy.png", 1, 2, true},         // L188–194
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			fs := &imageGrowthEnv{
				Env:           tools.LocalEnv(t.TempDir()),
				minimumLength: len(png), appendBytes: make([]byte, tc.appendBytes), repeatMutation: tc.repeatMutation,
			}
			if err := fs.Env.WriteFile(ctx, tc.path, png); err != nil {
				t.Fatal(err)
			}
			out, err := tools.Read(fs).Execute(ctx, durable.JSON{"path": tc.path}, nil)
			wantReads := []int{len(png), len(png) + tc.appendBytes}
			if !reflect.DeepEqual(fs.wholeReads, wantReads) {
				t.Errorf("whole-image read requests = %v, want %v", fs.wholeReads, wantReads)
			}
			if fs.opens != 1 || fs.closes != 1 || fs.appends != tc.appends {
				t.Errorf("opens=%d closes=%d appends=%d, want 1/1/%d", fs.opens, fs.closes, fs.appends, tc.appends)
			}
			expectedDisk := append(bytes.Clone(png), make([]byte, tc.appends*tc.appendBytes)...)
			onDisk, diskErr := fs.Env.ReadBinaryFile(ctx, tc.path)
			if diskErr != nil || !bytes.Equal(onDisk, expectedDisk) {
				t.Errorf("disk length=%d err=%v, want the original PNG with exactly %d zero bytes appended", len(onDisk), diskErr, tc.appends*tc.appendBytes)
			}
			if tc.repeatMutation {
				if err == nil || err.Error() != "busy.png changed while it was read" || len(out.Blocks) != 0 {
					t.Errorf("busy read: blocks=%d err=%v, want exact rejection and no image", len(out.Blocks), err)
				}
				return
			}
			want := []goai.ContentBlock{{Type: "image", MimeType: "image/png", Data: base64.StdEncoding.EncodeToString(expectedDisk)}}
			if err != nil || out.IsError || !reflect.DeepEqual(out.Blocks, want) {
				t.Errorf("growing read did not return one complete post-append PNG: result=%+v err=%v", out, err)
			}
		})
	}
}

// Only observe/interpose positional calls; the real local reader supplies bytes,
// metadata, and close behavior, and the real local writer appends to the file.
type imageGrowthEnv struct {
	*tools.Env
	minimumLength          int
	appendBytes            []byte
	repeatMutation         bool
	wholeReads             []int
	opens, closes, appends int
}

func (e *imageGrowthEnv) OpenBinaryReader(ctx context.Context, path string, noFollow bool) (durable.BinaryReader, error) {
	r, err := e.Env.OpenBinaryReader(ctx, path, noFollow)
	if err != nil {
		return nil, err
	}
	e.opens++
	return &imageGrowthReader{BinaryReader: r, env: e, path: path}, nil
}

type imageGrowthReader struct {
	durable.BinaryReader
	env  *imageGrowthEnv
	path string
}

func (r *imageGrowthReader) Read(ctx context.Context, offset int64, length int) ([]byte, error) {
	data, err := r.BinaryReader.Read(ctx, offset, length)
	if err != nil {
		return nil, err
	}
	if offset == 0 && length >= r.env.minimumLength {
		r.env.wholeReads = append(r.env.wholeReads, length)
		if r.env.appends == 0 || r.env.repeatMutation {
			if err := r.env.Env.AppendFile(ctx, r.path, r.env.appendBytes); err != nil {
				return nil, err
			}
			r.env.appends++
		}
	}
	return data, nil
}

func (r *imageGrowthReader) Close(ctx context.Context) error {
	r.env.closes++
	return r.BinaryReader.Close(ctx)
}
