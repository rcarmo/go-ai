package goai

import (
	"errors"
	"strings"
	"testing"
)

func TestValidationRegexMemoScope(t *testing.T) {
	schema := map[string]interface{}{"type": "string", "format": "regex"}
	for call := 0; call < 2; call++ {
		v := newToolSchemaValidator(schema)
		v.work = &toolSchemaWork{}
		for i := 0; i < 128; i++ {
			if !v.schemaAcceptsValue(`(?<x>a)\k<x>`, schema) || v.schemaAcceptsValue("[", schema) {
				t.Fatal("non-coercing membership changed")
			}
			issue := v.issue("[", schema, "v", make(map[toolSchemaMemoKey]bool))
			if issue == nil || issue.reason != "expected format regex" {
				t.Fatalf("issue=%v", issue)
			}
		}
		if v.work.regexChecks != 2 || len(v.regex) != 2 {
			t.Fatalf("call %d checks=%d cached=%d", call, v.work.regexChecks, len(v.regex))
		}
	}
}

func TestValidationRegexAnnotationsAndFailure(t *testing.T) {
	schema := map[string]interface{}{"format": "regex"}
	v := newToolSchemaValidator(schema)
	v.work = &toolSchemaWork{}
	for _, value := range []interface{}{nil, true, 42, []interface{}{}, map[string]interface{}{}} {
		if !v.schemaAcceptsValue(value, schema) {
			t.Fatalf("non-string annotation rejected %v", value)
		}
	}
	if v.regex != nil || v.work.regexChecks != 0 {
		t.Fatal("annotation invoked regex compiler")
	}
	// Infrastructure failures must reject rather than become an annotation or
	// silently masquerade as a RegExp constructor's false result.
	v.regex = map[string]toolSchemaRegexResult{"a": {err: errors.New("compiler unavailable")}}
	if v.schemaAcceptsValue("a", schema) {
		t.Fatal("unavailable checker accepted")
	}
	if issue := v.issue("a", schema, "v", make(map[toolSchemaMemoKey]bool)); issue == nil || !strings.HasPrefix(issue.reason, "regex format checker unavailable:") {
		t.Fatalf("issue=%v", issue)
	}
}
