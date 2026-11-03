package goai_test

import (
	"encoding/json"
	"reflect"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

// Exact metadata witnesses from the official pi-ai1.5.0 npm archive. The
// regeneration gate independently compares all public records with that archive.
func TestOfficial150CatalogueMetadata(t *testing.T) {
	goai.RegisterBuiltinModels()
	goai.RegisterBuiltinImageModels()
	response := goai.GetModel(goai.Provider("opencode"), "grok-build-0.1")
	if response == nil || response.ResponsesCompat == nil || response.ResponsesCompat.SupportsReasoningEffort == nil || *response.ResponsesCompat.SupportsReasoningEffort {
		t.Fatal("explicit false Responses effort metadata lost", response)
	}
	image := goai.GetImageModel(goai.ImageProvider("openrouter"), "recraft/recraft-v4.1-vector")
	if image == nil || !reflect.DeepEqual(image.Output, []string{"image"}) {
		t.Fatal("image output metadata lost", image)
	}
	image.Output[0] = "changed"
	if got := goai.GetImageModel(goai.ImageProvider("openrouter"), "recraft/recraft-v4.1-vector"); !reflect.DeepEqual(got.Output, []string{"image"}) {
		t.Fatal("image output aliases registry", got.Output)
	}
	flint := goai.GetModel(goai.Provider("radius"), "recursive/flint")
	if flint == nil || flint.Free == nil || !*flint.Free {
		t.Fatal("free metadata lost", flint)
	}
	*flint.Free = false
	if got := goai.GetModel(goai.Provider("radius"), "recursive/flint"); got.Free == nil || !*got.Free {
		t.Fatal("free metadata aliases registry", got.Free)
	}
	for _, value := range []any{response, image, flint} {
		if _, err := json.Marshal(value); err != nil {
			t.Fatal(err)
		}
	}
}
