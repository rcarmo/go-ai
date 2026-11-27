package durable

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

const v160WrappedPNG = "iVBORw0KGgo="

func v160LargePNGData() string { return "iVBORw0KGgoA" + strings.Repeat("QUJD", 256*1024) }

func v160LargeImageScript(data string) string {
	return `image("data:image/png;base64,iVBORw0K\r\nGgo=\n");
image("data:image/png;base64,` + data + `");`
}

type v160ImageFingerprint struct {
	Keys     []string
	Type     string
	MimeType string
	Bytes    int
	SHA256   string
}

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:192–204.
// This compares the image() output bytes and MIME, not a decoded image.
func TestSourceV160CodemodePublishedLargeImageData(t *testing.T) {
	checkPublishedLargeImageData(t, officialWorkerPackageRootV160(t), v160LargeImageScript(v160LargePNGData()))
}

func checkPublishedLargeImageData(t *testing.T, root, source string) {
	t.Helper()
	large := v160LargePNGData()
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "node", "testdata/worker-large-image-bytes-v160.mjs", placement)
			cmd.Env = append(os.Environ(), "GOAI_WORKER_PACKAGE_ROOT="+root)
			cmd.Stdin = strings.NewReader(source)
			raw, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("published image probe failed: %v\n%s", err, raw)
			}
			var got struct {
				OK     bool
				Error  string
				Output []v160ImageFingerprint
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("published image probe returned invalid receipt: %v\n%s", err, raw)
			}
			if !got.OK || got.Error != "" || len(got.Output) != 2 { // L199
				t.Fatalf("published images ok=%v error=%q outputs=%d; want two images", got.OK, got.Error, len(got.Output))
			}
			for i, data := range []string{v160WrappedPNG, large} {
				image := got.Output[i]
				if !slices.Equal(image.Keys, []string{"data", "mimeType", "type"}) || image.Type != "image" || image.MimeType != "image/png" { // L200–203
					t.Errorf("image %d output fields=%+v; want a PNG block", i, image)
				}
				if want := fmt.Sprintf("%x", sha256.Sum256([]byte(data))); image.Bytes != len(data) || image.SHA256 != want {
					t.Errorf("image %d output bytes differ: length=%d SHA-256=%s; want length=%d SHA-256=%s", i, image.Bytes, image.SHA256, len(data), want)
				}
			}
		})
	}
}

const v160GeneratedLargeImageScript = `
const data = "iVBORw0KGgoA" + "QUJD".repeat(256 * 1024);
image("data:image/png;base64,iVBORw0K\r\nGgo=\n");
image("data:image/png;base64," + data);`

func TestV160CodemodeNativeLargeImageData(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	large := v160LargePNGData()
	// The unchanged literal is above the native setup cap. Keep that
	// boundary separate from the ability to emit exactly the same bytes.
	literal := engine.Execute(ctx, v160LargeImageScript(large), quickjs.Options{})
	if literal.OK || literal.Error == nil || literal.Error.Kind != quickjs.ErrorSandbox || literal.Error.Message != "invalid or oversized QuickJS setup input" || len(literal.Output) != 0 {
		t.Fatalf("inline literal ok=%v error=%v outputs=%d; want source-size refusal", literal.OK, literal.Error, len(literal.Output))
	}
	got := engine.Execute(ctx, v160GeneratedLargeImageScript, quickjs.Options{})
	if !got.OK || got.Error != nil || len(got.Output) != 2 {
		t.Fatalf("inline images ok=%v error=%v outputs=%d; want two images", got.OK, got.Error, len(got.Output))
	}
	for i, data := range []string{v160WrappedPNG, large} {
		image := got.Output[i]
		if image.Type != "image" || image.MimeType != "image/png" || image.Text != "" {
			t.Errorf("image %d type=%q MIME=%q text bytes=%d; want a PNG block", i, image.Type, image.MimeType, len(image.Text))
		}
		if image.Data != data {
			t.Errorf("image %d output bytes differ: length=%d SHA-256=%x; want length=%d SHA-256=%x", i, len(image.Data), sha256.Sum256([]byte(image.Data)), len(data), sha256.Sum256([]byte(data)))
		}
	}
}
