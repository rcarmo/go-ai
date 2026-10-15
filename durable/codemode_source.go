package durable

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const defaultCodemodeTimeoutMs int64 = 300000
const defaultCodemodeOutputTokens = 10000

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
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return result, reject("codemode input is empty")
	}
	lines := strings.Split(input, "\n")
	line := strings.TrimSpace(lines[0])
	directive := "// @options:"
	if strings.HasPrefix(line, directive) {
		raw := strings.TrimSpace(strings.TrimPrefix(line, directive))
		opts, err := parseCodemodeOptions(raw)
		if err != nil {
			return result, err
		}
		for key, value := range opts {
			// JSON.parse rounds to a JavaScript Number before isSafeInteger.
			// Match that contract, including underflow and rounded decimals.
			var decoded any
			if err := json.Unmarshal(value, &decoded); err != nil {
				return result, reject("codemode option must be a safe integer")
			}
			number, ok := decoded.(float64)
			if !ok || math.IsNaN(number) || math.IsInf(number, 0) || math.Trunc(number) != number || math.Abs(number) > float64(MaxID) {
				return result, reject("codemode option must be a safe integer")
			}
			integer := int64(number)
			switch key {
			case "max_output_tokens":
				if integer < 0 || uint64(integer) > MaxID {
					return result, reject("invalid codemode output limit")
				}
				result.MaxOutputTokens = int(integer)
			case "timeout_ms":
				if integer <= 0 || integer > 2147483647 {
					return result, reject("invalid codemode timeout")
				}
				result.TimeoutMs = integer
			default:
				return result, reject("unknown codemode option")
			}
		}
		lines[0] = ""
		result.Code = strings.Join(lines, "\n")
		if strings.TrimSpace(result.Code) == "" {
			return result, reject("codemode options require JavaScript")
		}
	}
	for i, line := range lines {
		if i > 0 && strings.HasPrefix(strings.TrimSpace(line), directive) {
			return result, reject(fmt.Sprintf("The // @options: line must be the first line of the input; found one on line %d", i+1))
		}
	}
	return result, nil
}
