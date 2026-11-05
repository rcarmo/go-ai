package goai

import (
	"context"
	"fmt"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

type toolSchemaPatternKey struct{ pattern, value string }
type toolSchemaPatternResult struct {
	valid, matched bool
	err            error
}

// A single budget covers all distinct string-pattern evaluations in one
// validation call; both failures and successful results are memoised exactly.
func (v *toolSchemaValidator) stringPatternReason(value interface{}, schema map[string]interface{}) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	pattern, ok := schema["pattern"].(string)
	if !ok {
		return ""
	}
	result := v.matchSchemaPattern(pattern, text)
	if result.err != nil {
		return "pattern checker unavailable: " + result.err.Error()
	}
	if !result.valid {
		return "invalid regular expression: " + pattern
	}
	if !result.matched {
		return "expected to match " + pattern
	}
	return ""
}

func (v *toolSchemaValidator) matchSchemaPattern(pattern, text string) toolSchemaPatternResult {
	key := toolSchemaPatternKey{pattern, text}
	result, found := v.patterns[key]
	if !found {
		if v.patterns == nil {
			v.patterns = make(map[toolSchemaPatternKey]toolSchemaPatternResult)
		}
		if v.patternDeadline.IsZero() {
			v.patternDeadline = time.Now().Add(5 * time.Second)
		}
		engine, err := quickjs.Default()
		result.err = err
		if err == nil {
			ctx, cancel := context.WithDeadline(context.Background(), v.patternDeadline)
			result.valid, result.matched, result.err = engine.MatchRegExp(ctx, pattern, text)
			cancel()
		}
		v.patterns[key] = result
	}
	if v.patternFailure == nil {
		if result.err != nil {
			v.patternFailure = result.err
		} else if !result.valid {
			v.patternFailure = fmt.Errorf("invalid regular expression: %s", pattern)
		}
	}
	return result
}
