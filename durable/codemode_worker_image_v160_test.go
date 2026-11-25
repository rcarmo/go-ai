package durable_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rcarmo/go-ai/durable"
	"github.com/rcarmo/go-ai/durable/tools"
)

// Source: pi v1.6.0 codemode.test.ts:1043–1073. Images pass through the
// published guest and the public Go tool/result path, not a sandbox stub.
func TestSourceV160CodemodePublishedWorkerImageResults(t *testing.T) {
	const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
	savedImage := regexp.MustCompile(`\[Image saved to (\S+) \(image/png, \d+ bytes\)\]`)
	for _, tc := range []struct {
		name string
		read bool
	}{
		{"image-spill", false},
		{"read-image-and-text", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cwd := t.TempDir()
			spillRoot := t.TempDir()
			t.Setenv("TMPDIR", spillRoot)
			t.Setenv("TMP", spillRoot)
			env := tools.LocalEnv(cwd)
			code := `image("data:image/png;base64,` + png + `");`
			var reads int32
			var registrations []durable.ToolRegistration
			if tc.read {
				data, err := base64.StdEncoding.DecodeString(png)
				if err != nil {
					t.Fatal(err)
				}
				for path, bytes := range map[string][]byte{"pixel.png": data, "note.txt": []byte("hello")} {
					if err := env.WriteFile(t.Context(), path, bytes); err != nil {
						t.Fatal(err)
					}
				}
				pixel, err := json.Marshal(filepath.Join(cwd, "pixel.png"))
				if err != nil {
					t.Fatal(err)
				}
				note, err := json.Marshal(filepath.Join(cwd, "note.txt"))
				if err != nil {
					t.Fatal(err)
				}
				code = fmt.Sprintf(`const picture = await tools.read({ path: %s });
text([picture.type, picture.mimeType, picture.data.length].join(" "));
image(picture);
text(await tools.read({ path: %s }));`, pixel, note)
				reads = 2
				registrations = []durable.ToolRegistration{tools.Read(nil)}
			}
			text, result := durable.CheckV160PublishedWorkerOutput(t, code, env, reads, registrations...)
			matches := savedImage.FindStringSubmatch(text)
			if len(matches) != 2 {
				t.Fatalf("result lacks the saved image label: %q", text)
			}
			data, err := os.ReadFile(matches[1])
			if err != nil || base64.StdEncoding.EncodeToString(data) != png {
				t.Fatalf("saved image at %q differs from pinned PNG: %v", matches[1], err)
			}
			images := 0
			for _, block := range result.Content {
				if block.Type == "image" {
					images++
					if block.MimeType != "image/png" || block.Data != png {
						t.Errorf("unexpected image block: %+v", block)
					}
				}
			}
			if images != 1 {
				t.Errorf("image blocks = %d, want one", images)
			}
			if tc.read && (!strings.Contains(text, fmt.Sprintf("image image/png %d", len(png))) || !strings.Contains(text, "hello")) {
				t.Errorf("read result lacks the pinned image properties or text file: %q", text)
			}
		})
	}
}
