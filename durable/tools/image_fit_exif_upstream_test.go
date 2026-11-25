package tools_test

import (
	"bytes"
	"encoding/base64"
	"image"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable/tools"
)

// Exact withOrientation(encode(40, 30, "jpeg"), 6) from the pinned worker request.
const fitOrientationJPEG = "/9j/4QAiRXhpZgAATU0AKgAAAAgAAQESAAMAAAABAAYAAAAAAAD/4AAQSkZJRgABAgAAAQABAAD/wAARCAAeACgDAREAAhEBAxEB/9sAQwACAQEBAQECAQEBAgICAgIEAwICAgIFBAQDBAYFBgYGBQYGBgcJCAYHCQcGBggLCAkKCgoKCgYICwwLCgwJCgoK/9sAQwECAgICAgIFAwMFCgcGBwoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoK/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwD40vLy71G8l1DULqWe4nlaSeeaQs8jscszMeSSSSSeSa/kynThSgoQSSSsktEktkl2P97cryvLMjyyhluW0IUMPQhGnSpU4xhTp04RUYQhCKUYQhFKMYxSjGKSSSRFVneFABQAUAFABQAUAFABQAUAFABQAUAFABQAUAFABQAUAf/Z"

func TestSourceV160FitEXIFOrientation(t *testing.T) {
	limits := tools.ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 4718592}
	input := []goai.ContentBlock{{Type: "image", Data: fitOrientationJPEG, MimeType: "image/jpeg"}}
	original := append([]goai.ContentBlock(nil), input...)
	stored, err := base64.StdEncoding.DecodeString(fitOrientationJPEG)
	if err != nil {
		t.Fatal(err)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(stored))
	if err != nil || format != "jpeg" || cfg.Width != 40 || cfg.Height != 30 || len(fitOrientationJPEG) > limits.MaxBase64 {
		t.Fatalf("stored fixture is not an in-limit 40×30 JPEG: format=%q config=%+v err=%v", format, cfg, err)
	}
	got := tools.FitImages(t.Context(), input, limits, tools.NativeImageProcessor)
	if !got.Changed || len(got.Content) != 1 || got.Content[0].Type != "image" || got.Content[0].MimeType != "image/jpeg" {
		t.Fatalf("orientation did not produce exactly one fitted JPEG: changed=%v blocks=%d", got.Changed, len(got.Content))
	}
	if len(got.Content[0].Data) > limits.MaxBase64 {
		t.Fatal("upright output exceeds its base64 cap")
	}
	payload, err := base64.StdEncoding.DecodeString(got.Content[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	decoded, format, err := image.Decode(bytes.NewReader(payload))
	if err != nil || format != "jpeg" {
		t.Fatalf("upright payload is not a decodable JPEG: format=%q err=%v", format, err)
	}
	if size := decoded.Bounds().Size(); size != image.Pt(30, 40) { // images-fit.test.ts:125
		t.Fatalf("upright dimensions = %v, want (30,40)", size)
	}
	if !reflect.DeepEqual(input, original) || len(got.Notes) != 0 {
		t.Errorf("orientation mutated input or reported resize/conversion: notes=%q", got.Notes)
	}
}
