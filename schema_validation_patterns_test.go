package goai

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidationStringPatternMemoAndFailure(t *testing.T) {
	schema := map[string]interface{}{"type": "string", "pattern": `^a+$`}
	v := newToolSchemaValidator(schema)
	for i := 0; i < 3; i++ {
		if !v.schemaAcceptsValue("aa", schema) || v.schemaAcceptsValue("ab", schema) {
			t.Fatal("pattern membership changed")
		}
		issue := v.issue("ab", schema, "s", make(map[toolSchemaMemoKey]bool))
		if issue == nil || issue.Error() != "s: expected to match ^a+$" {
			t.Fatalf("issue=%v", issue)
		}
	}
	if len(v.patterns) != 2 {
		t.Fatalf("memo entries=%d", len(v.patterns))
	}
	v.patterns[toolSchemaPatternKey{`^a+$`, "aa"}] = toolSchemaPatternResult{err: errors.New("engine unavailable")}
	if v.schemaAcceptsValue("aa", schema) || !strings.Contains(v.stringPatternReason("aa", schema), "engine unavailable") {
		t.Fatal("engine failure accepted")
	}
}

func TestValidationPatternPropertyFailureIsNotAccepted(t *testing.T) {
	schema := map[string]interface{}{"type": "object", "patternProperties": map[string]interface{}{`(?<!x)y`: true}}
	v := newToolSchemaValidator(schema)
	v.patterns = map[toolSchemaPatternKey]toolSchemaPatternResult{{`(?<!x)y`, "y"}: {err: errors.New("engine unavailable")}}
	value := map[string]interface{}{"y": 1}
	if v.schemaAcceptsValue(value, schema) {
		t.Fatal("pattern property engine failure accepted")
	}
	issue := v.issue(value, schema, "v", make(map[toolSchemaMemoKey]bool))
	if issue == nil || !strings.Contains(issue.Error(), "pattern property checker") {
		t.Fatalf("issue=%v", issue)
	}
}

func TestValidationStringPatternDeadlineFailClosed(t *testing.T) {
	schema := map[string]interface{}{"pattern": `^(a+)+$`}
	v := newToolSchemaValidator(schema)
	v.patternDeadline = time.Now().Add(-time.Second)
	if v.schemaAcceptsValue("aaaa!", schema) || !strings.Contains(v.stringPatternReason("aaaa!", schema), "pattern checker unavailable") {
		t.Fatal("expired pattern budget accepted")
	}
}
