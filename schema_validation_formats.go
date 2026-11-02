package goai

import (
	"context"
	"time"

	"github.com/rcarmo/go-ai/internal/quickjs"
)

type toolSchemaRegexResult struct {
	valid bool
	err   error
}

// Format assertions apply only to strings. Unknown names are annotations, not
// failures. Keep this predicate non-coercing: it participates in whole-union
// membership as well as the final check, not in primitive conversion.
func (v *toolSchemaValidator) stringFormatReason(value interface{}, schema map[string]interface{}) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	name, _ := schema["format"].(string)
	var valid bool
	switch name {
	case "regex":
		result, found := v.regex[text]
		if !found {
			if v.regex == nil {
				v.regex = make(map[string]toolSchemaRegexResult)
			}
			if v.work != nil {
				v.work.regexChecks++
			}
			engine, err := quickjs.Default()
			result.err = err
			if err == nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				result.valid, result.err = engine.CheckRegExp(ctx, text)
				cancel()
			}
			v.regex[text] = result
		}
		if result.err != nil {
			return "regex format checker unavailable: " + result.err.Error()
		}
		valid = result.valid
	case "hostname":
		valid = schemaFormatHostname(text)
	case "date":
		valid = schemaFormatDate(text)
	case "time":
		valid = schemaFormatTime(text)
	case "date-time":
		valid = schemaFormatDateTime(text)
	default:
		pattern := schemaFormatPatterns[name]
		if pattern == nil {
			return ""
		}
		valid = pattern.MatchString(schemaFormatASCIIToLower(text))
	}
	if valid {
		return ""
	}
	return "expected format " + name
}

// JS /i without /u does not fold non-ASCII Kelvin sign or long s into ASCII.
// Only the comparison copy is folded; successful values retain their casing.
func schemaFormatASCIIToLower(value string) string {
	for i := 0; i < len(value); i++ {
		if value[i] >= 'A' && value[i] <= 'Z' {
			lower := []byte(value)
			for j := i; j < len(lower); j++ {
				if lower[j] >= 'A' && lower[j] <= 'Z' {
					lower[j] += 'a' - 'A'
				}
			}
			return string(lower)
		}
	}
	return value
}
