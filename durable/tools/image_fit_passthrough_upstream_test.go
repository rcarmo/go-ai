package tools_test

import (
	"context"
	"encoding/base64"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/durable/tools"
)

// Exact encode(8, 6, "png") from the pinned Photon request at images-fit.test.ts:90.
const fitPassthroughPNG = "iVBORw0KGgoAAAANSUhEUgAAAAgAAAAGCAYAAAD+Bd/7AAAAUElEQVR4Ae3AA6AkWZbG8f937o3IzKdyS2Oubdu2bdu2bdu2bWmMnpZKr54yMyLu+Xa3anqmhztr1a9ibPGCUC1eGCovHJUXjsoLR+WF4x8B9TAEF4Sg4YsAAAAASUVORK5CYII="

func TestSourceV160FitPassthrough(t *testing.T) {
	limits := tools.ImageLimits{MaxWidth: 2000, MaxHeight: 2000, MaxBase64: 4718592}
	type call struct {
		base64, mime string
		limits       tools.ImageLimits
	}
	var calls []call
	processor := func(ctx context.Context, data []byte, mime string, actual tools.ImageLimits) (*tools.PreparedImage, error) {
		calls = append(calls, call{base64.StdEncoding.EncodeToString(data), mime, actual})
		return tools.NativeImageProcessor(ctx, data, mime, actual)
	}
	mixed := []goai.ContentBlock{{Type: "text", Text: "a"}, {Type: "image", Data: fitPassthroughPNG, MimeType: "image/png"}}
	original := append([]goai.ContentBlock(nil), mixed...)
	wantCalls := []call{{fitPassthroughPNG, "image/png", limits}}
	if got := tools.FitImages(t.Context(), mixed, limits, processor); !reflect.DeepEqual(got, tools.FittedImages{}) { // L91
		t.Errorf("unchanged mixed content reported a fitted result: changed=%v content=%d notes=%q", got.Changed, len(got.Content), got.Notes)
	}
	if !reflect.DeepEqual(calls, wantCalls) { // L92
		t.Errorf("mixed content sent %d processor requests; want only the pinned PNG at default limits", len(calls))
	}
	if !reflect.DeepEqual(mixed, original) {
		t.Error("fitting mutated caller content")
	}
	if got := tools.FitImages(t.Context(), []goai.ContentBlock{{Type: "text", Text: "only text"}}, limits, processor); !reflect.DeepEqual(got, tools.FittedImages{}) { // L93–95
		t.Errorf("text-only content reported a fitted result: changed=%v content=%d notes=%q", got.Changed, len(got.Content), got.Notes)
	}
	if !reflect.DeepEqual(calls, wantCalls) { // L96
		t.Errorf("text-only content changed the processor request record: calls=%d", len(calls))
	}
}
