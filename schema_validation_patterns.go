package goai

import (
	"context"
	"fmt"
	"regexp/syntax"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

type toolSchemaPatternKey struct{ pattern, value string }
type toolSchemaPatternResult struct {
	valid, matched bool
	err            error
}

// checkSchemaPatternSyntax is the bounded compile-only building block for a
// future source-shaped schema compiler. It does not evaluate a candidate value.
func (v *toolSchemaValidator) checkSchemaPatternSyntax(pattern string) error {
	if err, ok := v.patternSyntax[pattern]; ok {
		return err
	}
	if v.patternSyntax == nil {
		v.patternSyntax = make(map[string]error)
	}
	if v.patternDeadline.IsZero() {
		v.patternDeadline = time.Now().Add(5 * time.Second)
	}
	ctx, cancel := context.WithDeadline(context.Background(), v.patternDeadline)
	defer cancel()
	err := ctx.Err()
	if err == nil {
		if v.work != nil {
			v.work.regexChecks++
		}
		var engine *quickjs.Engine
		engine, err = quickjs.Default()
		if err == nil {
			var valid bool
			valid, err = engine.CheckSchemaRegExp(ctx, pattern)
			if err == nil && !valid {
				err = &toolSchemaPatternSyntaxError{pattern: pattern}
			}
		}
	}
	v.patternSyntax[pattern] = err
	return err
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
			v.patternFailure = &toolSchemaPatternSyntaxError{pattern: pattern}
		}
	}
	return result
}

// Keep engine/infrastructure failures distinct from invalid schema syntax.
type toolSchemaPatternSyntaxError struct{ pattern string }

func (e *toolSchemaPatternSyntaxError) Error() string {
	return "invalid regular expression: " + e.pattern
}
func (e *toolSchemaPatternSyntaxError) publicMessage() string {
	reason := "Invalid regular expression"
	if _, err := syntax.Parse(e.pattern, syntax.Perl); err != nil {
		if parse, ok := err.(*syntax.Error); ok {
			switch parse.Code {
			case syntax.ErrMissingBracket:
				reason = "Unterminated character class"
			case syntax.ErrMissingParen:
				reason = "Unterminated group"
			case syntax.ErrUnexpectedParen:
				reason = "Unmatched )"
			case syntax.ErrMissingRepeatArgument:
				reason = "Nothing to repeat"
			case syntax.ErrInvalidCharRange:
				reason = "Range out of order in character class"
			}
		}
	}
	return fmt.Sprintf("Invalid regular expression: /%s/: %s", e.pattern, reason)
}
