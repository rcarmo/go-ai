package durable

import (
	"encoding/base64"
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func TestCodemode120ReadSizedImageFitsResultBudget(t *testing.T) {
	// Upstream read allows 4.5MiB of base64 image data. The codemode result
	// boundary must not reject that valid read result at a lower 4MiB ceiling.
	data := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("abc", (9<<19)/4)))
	if len(data) != 9<<19 {
		t.Fatal(len(data))
	}
	image := goai.ImageContent{Type: "image", MimeType: "image/png", Data: data}
	if err := validateCodemodeImages([]goai.ImageContent{image}, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if _, err := ownCodemodeResult(CodemodeResult{Images: []goai.ImageContent{image}}, DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}
