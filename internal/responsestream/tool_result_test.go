package responsestream

import (
	goai "github.com/rcarmo/go-ai"
	"reflect"
	"testing"
)

func TestToolResultOutputMain(t *testing.T) {
	image := goai.ContentBlock{Type: "image", MimeType: "image/png", Data: "AA=="}
	text := func(s string) goai.ContentBlock { return goai.ContentBlock{Type: "text", Text: s} }
	wireImage := map[string]any{"type": "input_image", "detail": "auto", "image_url": "data:image/png;base64,AA=="}
	for _, tc := range []struct {
		name    string
		content []goai.ContentBlock
		images  bool
		want    any
	}{
		{name: "empty", want: "(no tool output)"},
		{name: "whitespace", content: []goai.ContentBlock{text("\t \u00a0\n")}, want: "(no tool output)"},
		{name: "bom", content: []goai.ContentBlock{text("\ufeff")}, want: "(no tool output)"},
		{name: "nel_is_text", content: []goai.ContentBlock{text("\u0085")}, want: "\u0085"},
		{name: "lines", content: []goai.ContentBlock{text(""), text("a"), text("b"), text("")}, want: "\na\nb\n"},
		{name: "unsupported_image", content: []goai.ContentBlock{image}, want: "(see attached image)"},
		{name: "unsupported_image_text", content: []goai.ContentBlock{image, text("a")}, want: "a"},
		{name: "image", content: []goai.ContentBlock{image}, images: true, want: []any{wireImage}},
		{name: "mixed", content: []goai.ContentBlock{image, text("a"), image, text("b")}, images: true, want: []any{map[string]any{"type": "input_text", "text": "a\nb"}, wireImage, wireImage}},
		{name: "image_bom", content: []goai.ContentBlock{text("\ufeff"), image}, images: true, want: []any{wireImage}},
		{name: "literal_replacement", content: []goai.ContentBlock{text("\ufffd")}, want: "\ufffd"},
		{name: "surrogate_text", content: []goai.ContentBlock{text("\xed\xbf\xbf")}, want: ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &goai.Model{Input: []string{"text"}}
			if tc.images {
				model.Input = append(model.Input, "image")
			}
			if got := ToolResultOutput(model, tc.content); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("output %#v; want %#v", got, tc.want)
			}
		})
	}
}
