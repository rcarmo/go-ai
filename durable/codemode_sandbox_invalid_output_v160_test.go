package durable

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

const v160InvalidOutputScript = `
const errors = [];
const circular = {};
circular.self = circular;
for (const run of [
	() => text(circular),
	() => image(""),
	() => image("https://example.com/a.png"),
	() => image("data:image/png,raw"),
	() => image({ type: "text", text: "x" }),
	() => image({ type: "image", data: "" }),
	() => image(42),
	() => image("data:image/png;base64,AAAA!"),
	() => image("data:image/png;base64,AAAAA"),
	() => image("data:image/png;base64,AA=A"),
	() => image("data:image/png;base64,"),
	() => image("data:image/png;base64,AAAA\n[Output truncated]"),
	() => image({ type: "image", data: "AAAA!", mimeType: "image/png" }),
	() => image("data:image/png;base64,AAAA"),
	() => image("data:image/png;base64,QUJD"),
	() => image("data:image/jpeg;base64,/9j/9w=="),
]) {
	try { run(); errors.push("no error"); } catch (error) { errors.push(error.name + ": " + error.message); }
}
return errors;`

func checkV160InvalidSandboxOutput(t *testing.T, ok bool, outputs int, value json.RawMessage) {
	t.Helper()
	if !ok || outputs != 0 { // L172/L174
		t.Errorf("invalid-output script: ok=%v outputs=%d; want all errors caught and no output", ok, outputs)
	}
	var errors []string
	if err := json.Unmarshal(value, &errors); err != nil || len(errors) != 16 {
		t.Fatalf("invalid-output diagnostics=%s; want all 16 errors: %v", value, err)
	}
	if !regexp.MustCompile(`^TypeError: .*circular`).MatchString(errors[0]) { // L176
		t.Errorf("circular text: %q; want TypeError identifying circular data", errors[0])
	}
	const imageType = `TypeError: image expects a non-empty image URL string, an object with image_url, or an image block { type: "image", data, mimeType }`
	want := []string{
		imageType,
		"TypeError: remote image URLs are not supported in tool outputs. Pass a base64 data URI instead",
		"TypeError: invalid image output. Pass a base64 data URI instead",
		`TypeError: image only accepts image blocks, got "text"`,
		"TypeError: image expected image block data",
		imageType,
	}
	want = append(want, slices.Repeat([]string{"TypeError: invalid image output. The image data is not valid base64 (truncated or corrupted?)"}, 6)...)
	want = append(want, slices.Repeat([]string{"TypeError: invalid image output. The image data is not a PNG, JPEG, GIF, or WebP image"}, 3)...)
	if !slices.Equal(errors[1:], want) { // L177–188
		t.Errorf("image diagnostics=%q; want exact ordered errors %q", errors[1:], want)
	}
}

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:144–189.
func TestSourceV160CodemodePublishedInvalidOutput(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			got := runV160PublishedGlobalFixture(t, root, placement, v160InvalidOutputScript, "error")
			if got.Output == nil {
				t.Error("published result omitted the empty output array")
			}
			checkV160InvalidSandboxOutput(t, got.OK, len(got.Output), got.Value)
		})
	}
}

func TestV160CodemodeNativeInvalidOutput(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	got := engine.Execute(ctx, v160InvalidOutputScript, quickjs.Options{})
	checkV160InvalidSandboxOutput(t, got.OK, len(got.Output), got.Value)
}
