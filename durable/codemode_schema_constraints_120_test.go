package durable

import (
	"strings"
	"testing"
)

func TestCodemode120NullableConstraintsAndExclusiveBounds(t *testing.T) {
	property := JSON{"minimum": 10, "maximum": 90, "format": "port", "anyOf": []any{JSON{"type": "integer", "minimum": 1, "maximum": 20, "format": "int32"}, JSON{"type": "null"}}}
	text := strings.Join(codemodeCommentLines(property), "\n")
	for _, want := range []string{"@integer", "@minimum 10", "@maximum 20", "@format port", "@format int32"} {
		if !strings.Contains(text, want) {
			t.Fatal(text, want)
		}
	}
	for _, bad := range []string{"@minimum 1\n", "@maximum 90"} {
		if strings.Contains(text, bad) {
			t.Fatal("unmerged bound", text)
		}
	}
	for _, tc := range []struct {
		schema       JSON
		want, absent string
	}{{JSON{"type": "number", "minimum": 0, "maximum": 100, "exclusiveMinimum": true, "exclusiveMaximum": true}, "@exclusiveMinimum 0", "@minimum"}, {JSON{"type": "number", "minimum": 1, "maximum": 10, "exclusiveMinimum": false, "exclusiveMaximum": false}, "@minimum 1", "@exclusive"}, {JSON{"type": []any{"integer", "null"}}, "@integer", "@exclusive"}} {
		text := strings.Join(codemodeCommentLines(tc.schema), "\n")
		if !strings.Contains(text, tc.want) || strings.Contains(text, tc.absent) {
			t.Fatal(text, tc)
		}
	}
}
