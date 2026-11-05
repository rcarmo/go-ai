package goai

import (
	"context"
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

func TestValidationInvalidPatternCannotBeHiddenByUnion(t *testing.T) {
	pattern := map[string]interface{}{"type": "string", "pattern": `[`}
	schema := map[string]interface{}{"anyOf": []interface{}{pattern, map[string]interface{}{"type": "string"}}}
	v := newToolSchemaValidator(schema)
	v.patterns = map[toolSchemaPatternKey]toolSchemaPatternResult{{`[`, "aaa"}: {valid: false}}
	_, err := v.coerceWithJSONSchema("aaa", schema)
	if err == nil || !strings.Contains(err.Error(), "invalid regular expression") {
		t.Fatalf("union hid invalid pattern: %v", err)
	}
}

func TestValidationPatternFailureCannotBeHiddenByUnion(t *testing.T) {
	pattern := map[string]interface{}{"type": "string", "pattern": `^a+$`}
	schema := map[string]interface{}{"anyOf": []interface{}{pattern, map[string]interface{}{"type": "string"}}}
	v := newToolSchemaValidator(schema)
	v.patterns = map[toolSchemaPatternKey]toolSchemaPatternResult{{`^a+$`, "aaa"}: {err: errors.New("engine unavailable")}}
	_, err := v.coerceWithJSONSchema("aaa", schema)
	if err == nil || !strings.Contains(err.Error(), "engine unavailable") {
		t.Fatalf("union hid infrastructure failure: %v", err)
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

// Schema compilation shares the match budget and memoizes syntax by pattern,
// independent of any input value. Invalid syntax and expiry fail closed.
func TestValidationPatternCompileBudgetAndMemo(t *testing.T) {
	v := newToolSchemaValidator(map[string]interface{}{})
	v.work = &toolSchemaWork{}
	if err := v.checkSchemaPatternSyntax(`\p{L}+`); err != nil {
		t.Fatal(err)
	}
	if err := v.checkSchemaPatternSyntax(`\p{L}+`); err != nil || v.work.regexChecks != 1 {
		t.Fatalf("compile cache err=%v checks=%d", err, v.work.regexChecks)
	}
	if err := v.checkSchemaPatternSyntax(`\a`); err != nil {
		t.Fatalf("legacy fallback rejected: %v", err)
	}
	if err := v.checkSchemaPatternSyntax(`[`); err == nil {
		t.Fatal("invalid pattern compiled")
	}
	expired := newToolSchemaValidator(map[string]interface{}{})
	expired.patternDeadline = time.Now().Add(-time.Second)
	if err := expired.checkSchemaPatternSyntax(`a`); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expired compile got %v", err)
	}
}
