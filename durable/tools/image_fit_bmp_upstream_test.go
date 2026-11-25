package tools_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable/tools"
)

// Exact bmp(4, 4) from the pinned Photon request at images-fit.test.ts:129.
const fitConversionBMP = "Qk1mAAAAAAAAADYAAAAoAAAABAAAAAQAAAABABgAAAAAADAAAAAAAAAAAAAAAAAAAAAAAAAAAAD/AAD/AAD/AAD/AAD/AAD/AAD/AAD/AAD/AAD/AAD/AAD/AAD/AAD/AAD/AAD/"

func TestSourceV160FitBMPConversion(t *testing.T) {
	limits := tools.ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 4718592}
	input := []goai.ContentBlock{{Type: "image", Data: fitConversionBMP, MimeType: "image/bmp"}}
	original := append([]goai.ContentBlock(nil), input...)
	calls := 0
	processor := func(ctx context.Context, data []byte, mime string, actual tools.ImageLimits) (*tools.PreparedImage, error) {
		calls++
		if mime != "image/bmp" || base64.StdEncoding.EncodeToString(data) != fitConversionBMP || actual != limits {
			t.Errorf("processor saw MIME %q or wrong bytes/limits; want the pinned BMP at default limits", mime)
		}
		return tools.NativeImageProcessor(ctx, data, mime, actual)
	}
	got := tools.FitImages(t.Context(), input, limits, processor)
	if !got.Changed || len(got.Content) != 1 || got.Content[0].Type != "image" || got.Content[0].MimeType != "image/png" { // L130
		t.Fatalf("BMP did not produce exactly one PNG: %+v", got)
	}
	if want := []string{"Converted from image/bmp to image/png."}; !reflect.DeepEqual(got.Notes, want) { // L131
		t.Errorf("conversion notes = %q, want %q", got.Notes, want)
	}
	data, err := base64.StdEncoding.DecodeString(got.Content[0].Data)
	if err != nil || len(got.Content[0].Data) > limits.MaxBase64 {
		t.Fatalf("converted payload does not satisfy the base64 contract: %v", err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(data))
	if err != nil || format != "png" || decoded.Bounds().Size() != image.Pt(4, 4) {
		t.Errorf("converted payload is not a decodable 4×4 PNG: format=%q error=%v", format, err)
	}
	if calls != 1 || !reflect.DeepEqual(input, original) {
		t.Errorf("processor calls=%d or input changed; want one call and unchanged caller content", calls)
	}
}
