package durable

import (
	"encoding/json"
	"strings"
)

func codemodeCommentLines(value any) []string { return codemodeCommentLinesMode(value, true) }
func codemodeCommentLinesMode(value any, tagsEnabled bool) []string {
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
	if !tagsEnabled {
		return lines
	}
	tags := codemodeConstraintTags(object)
	if value, ok := object["default"]; ok && value != nil {
		raw, err := json.Marshal(value)
		if err == nil {
			tags = append(tags, "@default "+string(raw))
		}
	}
	if object["deprecated"] == true {
		tags = append(tags, "@deprecated")
	}
	kept := []string{}
	for _, tag := range tags {
		if codemodeTextUnits(tag) > 80 {
			continue
		}
		tag = strings.NewReplacer("\r", "\\u000d", "\n", "\\u000a", "\u2028", "\\u2028", "\u2029", "\\u2029").Replace(tag)
		kept = append(kept, tag)
	}
	if len(kept) > 0 {
		lines = append(lines, "// "+strings.Join(kept, " "))
	}
	return lines
}
func codemodeIndent(text string) string { return "  " + strings.ReplaceAll(text, "\n", "\n  ") }
