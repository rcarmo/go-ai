package tools_test

import (
	"context"
	"encoding/base64"
	"reflect"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable/tools"
)

// The pinned encode(4, 4, "png") fixture, before the truncation at line 141.
const fitOmissionPNG = "iVBORw0KGgoAAAANSUhEUgAAAAQAAAAECAYAAACp8Z5+AAAASUlEQVR4Ae3AA6AkWZbG8f937o3IzKdyS2Oubdu2bdu2bdu2bWmMnpZKr54yMyLu+Xa3anqmhztr1a9ibHE/qsUDUXlOVJ4T/wj1aAQPQGq1fgAAAABJRU5ErkJggg=="

func TestSourceV160FitImageOmissions(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(fitOmissionPNG)
	if err != nil || len(png) <= 40 {
		t.Fatalf("invalid PNG fixture: bytes=%d err=%v", len(png), err)
	}
	const baseNote = "An image (image/png) cannot be prepared for the model and was left out"
	limits := tools.ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 4718592}
	for _, tc := range []struct {
		name, payload string
		keepText      bool
		admit         bool
	}{
		{"damaged-png", base64.StdEncoding.EncodeToString(png[:40]), true, true},                                           // L144
		{"not-an-image", base64.StdEncoding.EncodeToString([]byte("not an image at all, though it says so")), false, true}, // L146
		{"malformed-base64", "%%%", false, false},                                                                          // L148–149
	} {
		t.Run(tc.name, func(t *testing.T) {
			block := goai.ContentBlock{Type: "image", MimeType: "image/png", Data: tc.payload}
			input := []goai.ContentBlock{block}
			var want []goai.ContentBlock
			if tc.keepText {
				want = []goai.ContentBlock{{Type: "text", Text: "a"}}
				input = []goai.ContentBlock{want[0], block}
			}
			original := append([]goai.ContentBlock(nil), input...)
			calls := 0
			processor := func(ctx context.Context, bytes []byte, mime string, actual tools.ImageLimits) (*tools.PreparedImage, error) {
				calls++
				if base64.StdEncoding.EncodeToString(bytes) != tc.payload || mime != block.MimeType || actual != limits {
					t.Error("processor did not receive the pinned input and limits")
				}
				return tools.NativeImageProcessor(ctx, bytes, mime, actual)
			}
			got := tools.FitImages(t.Context(), input, limits, processor)
			if !got.Changed || len(got.Content) != len(want) || tc.keepText && !reflect.DeepEqual(got.Content, want) {
				t.Errorf("omission changed=%v content=%+v, want fitted with only %+v", got.Changed, got.Content, want)
			}
			if len(got.Notes) != 1 {
				t.Fatalf("omission notes = %v, want exactly one", got.Notes)
			}
			if tc.admit {
				if got.Notes[0] != baseNote+"." || calls != 1 {
					t.Errorf("unpreparable image note = %q and calls=%d; want exact plain note and one call", got.Notes[0], calls)
				}
			} else {
				// The original pins the prefix, not the platform's base64 error wording.
				if !strings.HasPrefix(got.Notes[0], baseNote+": ") {
					t.Errorf("malformed base64 note lost its decoding error: %q", got.Notes[0])
				}
				if calls != 0 {
					t.Errorf("malformed base64 reached the processor %d times", calls)
				}
			}
			if !reflect.DeepEqual(input, original) {
				t.Error("fitting mutated caller content")
			}
		})
	}
}
