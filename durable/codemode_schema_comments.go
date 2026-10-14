package durable

import (
	"encoding/json"
	"strings"
)

func codemodeCommentLines(value any) []string {
	object, ok := codemodeSchemaMap(value)
	if !ok {
		return nil
	}
	lines := []string{}
	if description, ok := object["description"].(string); ok {
		for _, line := range strings.FieldsFunc(description, func(r rune) bool { return r == '\r' || r == '\n' || r == '\u2028' || r == '\u2029' }) {
			if text := strings.TrimSpace(line); text != "" {
				lines = append(lines, "// "+text)
			}
		}
	}
	tags := []string{}
	if object["type"] == "integer" {
		tags = append(tags, "@integer")
	}
	for _, key := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf", "minLength", "maxLength", "pattern", "format", "minItems", "maxItems", "minProperties", "maxProperties"} {
		value, ok := object[key]
		if !ok {
			continue
		}
		if key == "exclusiveMinimum" || key == "exclusiveMaximum" {
			if _, boolean := value.(bool); boolean {
				continue
			}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			continue
		}
		text := string(raw)
		if s, ok := value.(string); ok {
			text = s
		}
		tags = append(tags, "@"+key+" "+text)
	}
	if object["uniqueItems"] == true {
		tags = append(tags, "@uniqueItems")
	}
	if value, ok := object["default"]; ok && value != nil {
		raw, err := json.Marshal(value)
		if err == nil {
			tags = append(tags, "@default "+string(raw))
		}
	}
	if object["deprecated"] == true {
		tags = append(tags, "@deprecated")
	}
	for _, tag := range tags {
		if codemodeTextUnits(tag) > 80 {
			continue
		}
		tag = strings.NewReplacer("\r", "\\r", "\n", "\\n", "\u2028", "\\u2028", "\u2029", "\\u2029").Replace(tag)
		lines = append(lines, "// "+tag)
	}
	return lines
}
func codemodeIndent(text string) string { return "  " + strings.ReplaceAll(text, "\n", "\n  ") }
func codemodeDropConstraintLines(text string) string {
	lines := strings.Split(text, "\n")
	out := []string{}
	for _, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "// @") {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
