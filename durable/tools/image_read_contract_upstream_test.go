package tools_test

import (
	"bytes"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable"
	"github.com/rcarmo/go-ai/durable/tools"
)

// Exact encoded fixtures from pi's pinned image-fixtures.ts, reproduced by
// probe-tools-images.sh read-contract and recorded in tools-images-read-contract.json.
const (
	readContractPNG8x6 = "iVBORw0KGgoAAAANSUhEUgAAAAgAAAAGCAYAAAD+Bd/7AAAAUElEQVR4Ae3AA6AkWZbG8f937o3IzKdyS2Oubdu2bdu2bdu2bWmMnpZKr54yMyLu+Xa3anqmhztr1a9ibPGCUC1eGCovHJUXjsoLR+WF4x8B9TAEF4Sg4YsAAAAASUVORK5CYII="
	readContractGIF    = "R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7"
	readContractPNG4x4 = "iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAYAAACp8Z5+AAAASUlEQVR4Ae3AA6AkWZbG8f937o3IzKdyS2Oubdu2bdu2bdu2bWmMnpZKr54yMyLu+Xa3anqmhztr1a9ibHE/qsUDUXlOVJ4T/wj1aAQPQGq1fgAAAABJRU5ErkJggg=="
)

func TestSourceV160ImageReadContract(t *testing.T) {
	t.Run("inline-byte-identity", func(t *testing.T) { // tools-images.test.ts:111–118
		ctx := t.Context()
		env := tools.LocalEnv(t.TempDir())
		read := tools.Read(env)
		for _, tc := range []struct{ path, mime, encoded string }{
			{"a.png", "image/png", readContractPNG8x6},
			{"a.gif", "image/gif", readContractGIF},
		} {
			if err := env.WriteFile(ctx, tc.path, readContractBytes(t, tc.encoded)); err != nil {
				t.Fatal(err)
			}
			result, err := read.Execute(ctx, durable.JSON{"path": tc.path}, nil)
			want := []goai.ContentBlock{{Type: "image", MimeType: tc.mime, Data: tc.encoded}}
			if err != nil || result.IsError || !reflect.DeepEqual(result.Blocks, want) {
				t.Errorf("%s: output=%+v isError=%v err=%v, want exactly %+v", tc.path, result.Blocks, result.IsError, err, want)
			}
			if tc.mime == "image/png" && (len(result.Diagnostics) != 1 || result.Diagnostics[0].Message != "Read image file [image/png].") {
				t.Errorf("PNG messages = %+v, want only the pinned read diagnostic", result.Diagnostics)
			}
		}
	})
	t.Run("description-formats", func(t *testing.T) { // tools-images.test.ts:166–169
		env := tools.LocalEnv(t.TempDir())
		for _, tc := range []struct{ description, formats string }{
			{tools.Read(env).Definition.Description, "(jpg, png, gif, webp)"},
			{tools.ReadWithImages(env).Definition.Description, "(jpg, png, gif, webp, bmp)"},
			{tools.ReadWithPreparedImages(env, tools.ImageLimits{}).Definition.Description, "(jpg, png, gif, webp, bmp)"},
		} {
			if !strings.Contains(tc.description, tc.formats) {
				t.Errorf("read description %q does not list %q", tc.description, tc.formats)
			}
		}
	})
	t.Run("not-even-one-pixel", func(t *testing.T) { // tools-images.test.ts:234–237
		data := readContractBytes(t, readContractPNG4x4)
		limits := tools.ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 8}
		got, err := tools.NativeImageProcessor(t.Context(), data, "image/png", limits)
		if err != nil || got != nil {
			t.Errorf("8-byte cap returned %v, err=%v; want no prepared image or error", got != nil, err)
		}
		// An unfit image is different from an invalid fixture or disabled processor.
		limits.MaxBase64 = 4718592
		got, err = tools.NativeImageProcessor(t.Context(), data, "image/png", limits)
		if err != nil || got == nil || !bytes.Equal(got.Data, data) {
			t.Fatalf("same PNG must pass through under the default cap: prepared=%v, err=%v", got != nil, err)
		}
	})
}

func readContractBytes(t *testing.T, encoded string) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
