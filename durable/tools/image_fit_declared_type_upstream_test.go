package tools_test

import (
	"context"
	"encoding/base64"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable/tools"
)

// The exact encode(8, 6, "jpeg") from the pinned Photon request at images-fit.test.ts:135.
const fitDeclaredJPEG = "/9j/4AAQSkZJRgABAgAAAQABAAD/wAARCAAGAAgDAREAAhEBAxEB/9sAQwACAQEBAQECAQEBAgICAgIEAwICAgIFBAQDBAYFBgYGBQYGBgcJCAYHCQcGBggLCAkKCgoKCgYICwwLCgwJCgoK/9sAQwECAgICAgIFAwMFCgcGBwoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoKCgoK/8QAHwAAAQUBAQEBAQEAAAAAAAAAAAECAwQFBgcICQoL/8QAtRAAAgEDAwIEAwUFBAQAAAF9AQIDAAQRBRIhMUEGE1FhByJxFDKBkaEII0KxwRVS0fAkM2JyggkKFhcYGRolJicoKSo0NTY3ODk6Q0RFRkdISUpTVFVWV1hZWmNkZWZnaGlqc3R1dnd4eXqDhIWGh4iJipKTlJWWl5iZmqKjpKWmp6ipqrKztLW2t7i5usLDxMXGx8jJytLT1NXW19jZ2uHi4+Tl5ufo6erx8vP09fb3+Pn6/8QAHwEAAwEBAQEBAQEBAQAAAAAAAAECAwQFBgcICQoL/8QAtREAAgECBAQDBAcFBAQAAQJ3AAECAxEEBSExBhJBUQdhcRMiMoEIFEKRobHBCSMzUvAVYnLRChYkNOEl8RcYGRomJygpKjU2Nzg5OkNERUZHSElKU1RVVldYWVpjZGVmZ2hpanN0dXZ3eHl6goOEhYaHiImKkpOUlZaXmJmaoqOkpaanqKmqsrO0tba3uLm6wsPExcbHyMnK0tPU1dbX2Nna4uPk5ebn6Onq8vP09fb3+Pn6/9oADAMBAAIRAxEAPwD40vLy71G8l1DULqWe4nlaSeeaQs8jscszMeSSSSSeSa/kynThSgoQSSSsktEktkl2P97cryvLMjyyhluW0IUMPQhGnSpU4xhTp04RUYQhCKUYQhFKMYxSjGKSSSR//9k="

func TestSourceV160FitDeclaredImageType(t *testing.T) {
	limits := tools.ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 4718592}
	input := []goai.ContentBlock{{Type: "image", Data: fitDeclaredJPEG, MimeType: "image/png"}}
	original := append([]goai.ContentBlock(nil), input...)
	calls := 0
	processor := func(ctx context.Context, data []byte, mime string, actual tools.ImageLimits) (*tools.PreparedImage, error) {
		calls++
		if mime != "image/jpeg" || base64.StdEncoding.EncodeToString(data) != fitDeclaredJPEG || actual != limits {
			t.Errorf("processor saw MIME %q or wrong bytes/limits; want the pinned JPEG at default limits", mime)
		}
		return tools.NativeImageProcessor(ctx, data, mime, actual)
	}
	got := tools.FitImages(t.Context(), input, limits, processor)
	want := []goai.ContentBlock{{Type: "image", Data: fitDeclaredJPEG, MimeType: "image/jpeg"}}
	if !got.Changed || !reflect.DeepEqual(got.Content, want) { // images-fit.test.ts:136
		t.Errorf("fitted output did not keep the exact bytes with the corrected JPEG MIME: changed=%v content=%+v", got.Changed, got.Content)
	}
	if len(got.Notes) != 0 {
		t.Errorf("type correction added notes: %q", got.Notes)
	}
	if calls != 1 || !reflect.DeepEqual(input, original) {
		t.Errorf("processor calls=%d or input changed; want one call and unchanged caller content", calls)
	}
}
