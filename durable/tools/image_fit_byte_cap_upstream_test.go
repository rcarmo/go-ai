package tools_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"os"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable/tools"
)

func TestSourceV160FitEncodedByteCap(t *testing.T) {
	// Use the actual pinned worker request, not another encoder's approximation
	// of the 64×64 noisy PNG from images-fit.test.ts:118.
	receipt, err := os.ReadFile("../../docs/upstream-v1.6.0/test-inventory/images-fit-byte-cap.json")
	if err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Fixture string `json:"fixtureBase64"`
	}
	if err := json.Unmarshal(receipt, &recorded); err != nil {
		t.Fatal(err)
	}
	limits := tools.ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 8000}
	data, err := base64.StdEncoding.DecodeString(recorded.Fixture)
	if err != nil {
		t.Fatal(err)
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "png" || config.Width != 64 || config.Height != 64 || len(recorded.Fixture) <= limits.MaxBase64 {
		t.Fatalf("fixture must be a 64×64 PNG over the encoded byte cap: config=%+v format=%q encoded=%d err=%v", config, format, len(recorded.Fixture), err)
	}
	input := []goai.ContentBlock{{Type: "image", Data: recorded.Fixture, MimeType: "image/png"}}
	original := append([]goai.ContentBlock(nil), input...)
	fitted := tools.FitImages(t.Context(), input, limits, tools.NativeImageProcessor)
	if !fitted.Changed || len(fitted.Content) != 1 || fitted.Content[0].Type != "image" {
		t.Fatalf("wanted one re-encoded image, changed=%v images=%d notes=%q", fitted.Changed, len(fitted.Content), fitted.Notes)
	}
	block := fitted.Content[0]
	if len(block.Data) > limits.MaxBase64 { // L119
		t.Errorf("fitted base64 length = %d, cap = %d", len(block.Data), limits.MaxBase64)
	}
	payload, err := base64.StdEncoding.DecodeString(block.Data)
	if err != nil {
		t.Fatal(err)
	}
	decoded, actual, err := image.Decode(bytes.NewReader(payload))
	if err != nil || block.MimeType != "image/"+actual || decoded.Bounds().Dx() > limits.MaxWidth || decoded.Bounds().Dy() > limits.MaxHeight {
		t.Fatalf("invalid or over-limit fitted payload: declared=%q actual=%q err=%v", block.MimeType, actual, err)
	}
	if len(fitted.Notes) == 0 { // L120
		t.Errorf("fitting produced no note: %d base64 bytes (%s %v) -> %d (%s %v)", len(recorded.Fixture), format, image.Pt(config.Width, config.Height), len(block.Data), actual, decoded.Bounds().Size())
	}
	if !reflect.DeepEqual(input, original) {
		t.Error("fitting mutated caller content")
	}
	t.Logf("base64 %d -> %d, output %s %v, notes=%q", len(recorded.Fixture), len(block.Data), block.MimeType, decoded.Bounds().Size(), fitted.Notes)
}
