package durable

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

const v160MixedOutputScript = `
console.log("hello", 1, { a: 1 });
text({ json: true });
text(undefined);
text(7);
image("data:image/png;base64,iVBORw0KGgo=");
image({ image_url: "data:image/jpeg;base64,/9j/4A==" });
image({ type: "image", data: "R0lGODlh", mimeType: "image/gif" });
image("data:image/png;base64,UklGRgAAAABXRUJQ");
image({ type: "image", data: "iVBORw0KGgo=" });
console.error(new Error("bad"));
return null;`

func v160MixedOutput() []quickjs.OutputItem {
	return []quickjs.OutputItem{
		{Type: "console", Text: `hello 1 {"a":1}`},
		{Type: "text", Text: `{"json":true}`},
		{Type: "text", Text: "undefined"},
		{Type: "text", Text: "7"},
		{Type: "image", Data: "iVBORw0KGgo=", MimeType: "image/png"},
		{Type: "image", Data: "/9j/4A==", MimeType: "image/jpeg"},
		{Type: "image", Data: "R0lGODlh", MimeType: "image/gif"},
		{Type: "image", Data: "UklGRgAAAABXRUJQ", MimeType: "image/webp"},
		{Type: "image", Data: "iVBORw0KGgo=", MimeType: "image/png"},
	}
}

// Source: pi v1.6.0 packages/codemode/test/sandbox.test.ts:97–130.
func TestSourceV160CodemodePublishedOutputOrder(t *testing.T) {
	root := officialWorkerPackageRootV160(t)
	var want []map[string]any
	for _, item := range v160MixedOutput() {
		if item.Type == "image" {
			want = append(want, map[string]any{"type": "image", "data": item.Data, "mimeType": item.MimeType})
		} else {
			row := map[string]any{"type": "text", "text": item.Text}
			if item.Type == "console" {
				row["console"] = true
			}
			want = append(want, row)
		}
	}
	for _, placement := range []string{"inline", "portable", "node-worker"} {
		t.Run(placement, func(t *testing.T) {
			got := runV160PublishedGlobalFixture(t, root, placement, v160MixedOutputScript, "error")
			if !got.OK || len(got.Output) != len(want)+1 { // L112
				t.Fatalf("output ok=%v error=%v blocks=%d; want ten blocks", got.OK, got.Error, len(got.Output))
			}
			if !reflect.DeepEqual(got.Output[:len(want)], want) { // L113–124
				t.Errorf("output=%+v; want exact ordered output %+v", got.Output[:len(want)], want)
			}
			last := got.Output[len(want)]
			text, _ := last["text"].(string)
			if last["type"] != "text" || last["console"] != true || !strings.HasPrefix(text, "Error: bad") { // L125–129
				t.Errorf("last block=%+v; want console text starting with Error: bad", last)
			}
		})
	}
}

func TestV160CodemodeNativeOutputOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	engine, err := quickjs.NewEngine(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(context.Background())
	want := v160MixedOutput()
	got := engine.Execute(ctx, v160MixedOutputScript, quickjs.Options{})
	if !got.OK || len(got.Output) != len(want)+1 {
		t.Fatalf("output ok=%v error=%v blocks=%d; want ten blocks", got.OK, got.Error, len(got.Output))
	}
	if !reflect.DeepEqual(got.Output[:len(want)], want) {
		t.Errorf("output=%+v; want exact ordered output %+v", got.Output[:len(want)], want)
	}
	last := got.Output[len(want)]
	if last.Type != "console" || !strings.HasPrefix(last.Text, "Error: bad") {
		t.Errorf("last block=%+v; want console text starting with Error: bad", last)
	}
}
