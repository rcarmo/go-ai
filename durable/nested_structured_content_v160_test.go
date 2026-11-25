package durable

import (
	"strings"
	"testing"

	goai "github.com/rcarmo/go-ai"
)

func testStructuredContentUnionV160() JSON {
	return JSON{"anyOf": []any{
		map[string]any{"type": "string"},
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"type": map[string]any{"type": "string", "enum": []any{"image"}}, "mimeType": map[string]any{"type": "string"}, "data": map[string]any{"type": "string"},
			},
			"required": []any{"type", "mimeType", "data"},
		},
	}}
}

func TestStructuredOutputUnionV160(t *testing.T) {
	schema := testStructuredContentUnionV160()
	if err := validateSchema(schema, 0); err != nil {
		t.Fatal(err)
	}
	image := JSON{"type": "image", "mimeType": "image/png", "data": "AAAA"}
	for _, value := range []any{"text", image} {
		if err := checkSchema(schema, value, 0); err != nil {
			t.Errorf("valid structured value %v rejected: %v", value, err)
		}
	}
	for _, value := range []any{true, JSON{"type": "text", "mimeType": "image/png", "data": "AAAA"}, JSON{"type": "image", "mimeType": "image/png"}} {
		if err := checkSchema(schema, value, 0); err == nil {
			t.Errorf("accepted value outside the union: %v", value)
		}
	}
	for _, malformed := range []JSON{
		{"anyOf": []any{}},
		{"anyOf": []any{"string"}},
		{"anyOf": []any{map[string]any{"type": "string"}}, "type": "number"},
	} {
		if err := validateSchema(malformed, 0); err == nil {
			t.Errorf("accepted malformed union: %v", malformed)
		}
	}
}

func TestProjectedStructuredOutputKeepsModelImageOutOfBoundedJSONV160(t *testing.T) {
	limits := DefaultLimits()
	data := strings.Repeat("AAAA", limits.MaxStringBytes/4+1)
	image := goai.ContentBlock{Type: "image", MimeType: "image/png", Data: data}
	result := (ToolResult{Blocks: []goai.ContentBlock{image}}).WithStructuredContent()
	result, err := detachToolResult(result, limits)
	if err != nil {
		t.Fatal(err)
	}
	offer := toolOffer{StructuredOutputSchema: testStructuredContentUnionV160()}
	h := Harness{session: &Session{limits: limits}}
	model := h.validateModelStructuredResult(result, offer, func(err error) { t.Error(err) })
	if model.IsError || model.HasStructuredOutput || len(model.Blocks) != 1 || model.Blocks[0].Data != data {
		t.Fatal("model receipt did not retain the detached image without larger structured JSON")
	}
	if nested, invalid := validateNestedStructuredResult(result, offer, limits); !invalid || nested.HasStructuredOutput || !nested.IsError {
		t.Fatal("structured nested output exceeded the unchanged JSON record limit")
	}

	explicit := (ToolResult{Content: "visible", StructuredOutput: "explicit", HasStructuredOutput: true}).WithStructuredContent()
	if got, invalid := validateNestedStructuredResult(explicit, offer, limits); invalid || got.StructuredOutput != "explicit" {
		t.Fatal("derived content replaced an explicit structured value")
	}
	failure := (ToolResult{Content: "failed", IsError: true}).WithStructuredContent()
	if got, invalid := validateNestedStructuredResult(failure, offer, limits); invalid || !got.IsError || got.HasStructuredOutput {
		t.Fatal("a failed tool was converted to a returned value")
	}
}
