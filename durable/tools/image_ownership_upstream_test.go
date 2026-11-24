package tools_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"reflect"
	"testing"

	"github.com/rcarmo/go-ai/durable/tools"
)

// TestSourceV160PreparedImageOwnership maps tools-images.test.ts:431–442.
// Go exercises native processor and fitting APIs, not a Photon worker.
func TestSourceV160PreparedImageOwnership(t *testing.T) {
	const size = 1100
	pixels := image.NewNRGBA(image.Rect(0, 0, size, size))
	var seed uint32 = 1
	for i := range pixels.Pix {
		// The original uses JS Number multiplication and addition before >>> 0.
		product := float64(float64(seed) * 1103515245)
		seed = uint32(uint64(product + 12345))
		pixels.Pix[i] = byte(seed >> 24)
		if i%4 == 3 {
			pixels.Pix[i] = 255
		}
	}
	t.Run("source-pixels", func(t *testing.T) {
		var encoded bytes.Buffer
		encoder := png.Encoder{CompressionLevel: png.NoCompression}
		if err := encoder.Encode(&encoded, pixels); err != nil {
			t.Fatal(err)
		}
		checkNativePreparedImageOwnership(t, encoded.Bytes(), size)
	})
	t.Run("exact-photon-input", func(t *testing.T) {
		path := os.Getenv("GO_AI_PINNED_PHOTON_INPUT")
		if path == "" {
			t.Skip("set GO_AI_PINNED_PHOTON_INPUT to the stable-worker probe fixture")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		const wantInput = "0ff500396af0d58acc92d2f9c308d18a9afafb15850fca7f3bf59f9e3bdb09c7"
		if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != wantInput {
			t.Fatalf("pinned Photon input digest = %s, want %s", got, wantInput)
		}
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil || decoded.Bounds() != pixels.Bounds() {
			t.Fatalf("pinned PNG does not have the expected dimensions: %v", err)
		}
		copied := image.NewNRGBA(pixels.Bounds())
		draw.Draw(copied, copied.Bounds(), decoded, image.Point{}, draw.Src)
		if !bytes.Equal(copied.Pix, pixels.Pix) {
			t.Fatalf("native fixture pixels differ from pinned input: native=%x pinned=%x", sha256.Sum256(pixels.Pix), sha256.Sum256(copied.Pix))
		}
		checkNativePreparedImageOwnership(t, data, size)
	})
}

func checkNativePreparedImageOwnership(t *testing.T, input []byte, dimension int) {
	t.Helper()
	const inputMIME = "image/png"
	limits := tools.ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 4718592}
	if base64.StdEncoding.EncodedLen(len(input)) <= limits.MaxBase64 {
		t.Fatal("fixture must exercise encoding rather than passthrough")
	}
	before := bytes.Clone(input)
	processor, err := tools.NativeImageProcessor(t.Context(), input, inputMIME, limits)
	if err != nil || processor == nil {
		t.Fatalf("native processor did not prepare an image: %v", err)
	}
	if !bytes.Equal(input, before) {
		t.Fatal("native processor changed caller PNG bytes")
	}
	inline, err := tools.PrepareImage(t.Context(), input, inputMIME, limits)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(input, before) {
		t.Fatal("inline preparation changed caller PNG bytes")
	}
	if !reflect.DeepEqual(*processor, inline) {
		t.Fatalf("prepared values differ: processor data sha256=%x, inline data sha256=%x", sha256.Sum256(processor.Data), sha256.Sum256(inline.Data))
	}
	if bytes.Equal(processor.Data, input) || processor.Width != dimension || processor.Height != dimension || processor.OriginalWidth != 0 || processor.OriginalHeight != 0 {
		t.Fatalf("expected re-encoding without resize metadata at %dx%d, got dimensions=%dx%d resize-from=%dx%d",
			dimension, dimension, processor.Width, processor.Height, processor.OriginalWidth, processor.OriginalHeight)
	}
	decoded, format, err := image.DecodeConfig(bytes.NewReader(processor.Data))
	if err != nil || decoded.Width != dimension || decoded.Height != dimension || "image/"+format != processor.MimeType {
		t.Fatalf("prepared payload does not match its MIME or %dx%d dimensions: format=%q config=%+v err=%v", dimension, dimension, format, decoded, err)
	}
	var wantConvertedFrom string
	var wantNotes []string
	if processor.MimeType != inputMIME {
		wantConvertedFrom = inputMIME
		wantNotes = []string{"Converted from image/png to " + processor.MimeType + "."}
	}
	fit := tools.FitImage(t.Context(), input, inputMIME, limits, tools.NativeImageProcessor)
	wantBase64 := base64.StdEncoding.EncodeToString(processor.Data)
	if fit.Image == nil || fit.Image.Type != "image" || fit.Image.MimeType != processor.MimeType || fit.Image.Data != wantBase64 || fit.OmittedNote != "" ||
		processor.ConvertedFrom != wantConvertedFrom || !reflect.DeepEqual(fit.Notes, wantNotes) || len(wantBase64) > limits.MaxBase64 {
		t.Fatal("fitted image body, conversion note, or model byte limit differs from the prepared value")
	}
	if !bytes.Equal(input, before) {
		t.Fatal("fitting changed caller PNG bytes")
	}
	t.Logf("native %s bytes=%d sha256=%x; base64 bytes=%d sha256=%x", processor.MimeType, len(processor.Data), sha256.Sum256(processor.Data), len(wantBase64), sha256.Sum256([]byte(wantBase64)))
}
