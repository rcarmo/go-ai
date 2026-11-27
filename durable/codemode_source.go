package durable

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"unicode"
)

const defaultCodemodeTimeoutMs int64 = 300000
const defaultCodemodeOutputTokens = 10000

// CodemodeSourceError identifies invalid Codemode source before a run is
// admitted. Its message matches the pinned JavaScript parser contract.
type CodemodeSourceError struct{ Message string }

func (e *CodemodeSourceError) Error() string { return e.Message }
func codemodeSourceError(message string) error {
	return &CodemodeSourceError{Message: message}
}

// CodemodeSource parses the pinned optional first-line @options directive.
// Source retains line numbering; arbitrary option fields and unsafe numbers
// reject before any durable run or sandbox admission.
type CodemodeSource struct {
	Code            string
	TimeoutMs       int64
	MaxOutputTokens int
}

func ParseCodemodeSource(input string) (CodemodeSource, error) {
	result := CodemodeSource{Code: input, TimeoutMs: defaultCodemodeTimeoutMs, MaxOutputTokens: defaultCodemodeOutputTokens}
	if strings.TrimSpace(input) == "" {
		return result, codemodeSourceError("Expected JavaScript source text (non-empty). Provide JS only, optionally with a first line `// @options: {\"max_output_tokens\": 1000}`.")
	}

	lines := strings.Split(input, "\n")
	directive := "// @options:"
	for i, text := range lines {
		if i > 0 && strings.HasPrefix(strings.TrimLeftFunc(text, unicode.IsSpace), directive) {
			return result, codemodeSourceError(fmt.Sprintf("The // @options: line must be the first line of the input; found one on line %d", i+1))
		}
	}

	firstLine := strings.TrimSuffix(lines[0], "\r")
	trimmed := strings.TrimLeftFunc(firstLine, unicode.IsSpace)
	if !strings.HasPrefix(trimmed, directive) {
		return result, nil
	}
	newline := strings.IndexByte(input, '\n')
	if newline == -1 {
		result.Code = ""
	} else {
		result.Code = input[newline:]
	}
	if strings.TrimSpace(result.Code) == "" {
		return result, codemodeSourceError("The @options line must be followed by JavaScript source on subsequent lines")
	}

	opts, err := parseCodemodeOptions(strings.TrimSpace(strings.TrimPrefix(trimmed, directive)))
	if err != nil {
		return result, err
	}
	for key, value := range opts {
		if key != "max_output_tokens" && key != "timeout_ms" {
			return result, codemodeSourceError("@options only supports " + codemodeSupportedFields + "; got `" + key + "`")
		}
		// JSON.parse rounds to a JavaScript Number before isSafeInteger.
		// Match that contract, including underflow and rounded decimals.
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return result, codemodeNumericSourceError(key)
		}
		number, ok := decoded.(float64)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || number < 0 || number > float64(MaxID) {
			return result, codemodeNumericSourceError(key)
		}
		integer := int64(number)
		switch key {
		case "max_output_tokens":
			result.MaxOutputTokens = int(integer)
		case "timeout_ms":
			if integer == 0 || integer > 2147483647 {
				return result, codemodeNumericSourceError(key)
			}
			result.TimeoutMs = integer
		}
	}
	return result, nil
}

func codemodeNumericSourceError(key string) error {
	if key == "timeout_ms" {
		return codemodeSourceError("@options field `timeout_ms` must be a positive integer up to 2147483647")
	}
	return codemodeSourceError("@options field `max_output_tokens` must be a non-negative safe integer")
}
