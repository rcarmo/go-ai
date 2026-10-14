package durable

import (
	"fmt"
	"math/big"
	"strconv"
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
			// JSON numbers with a decimal point/exponent may still be integers.
			// Keep exact bounds rather than rounding an unsafe float into range.
			rawNumber := strings.TrimSpace(string(value))
			if len(rawNumber) > 128 {
				return result, reject("codemode option number too long")
			}
			if at := strings.IndexAny(rawNumber, "eE"); at >= 0 {
				exponent, e := strconv.ParseInt(rawNumber[at+1:], 10, 32)
				if e != nil || exponent < -400 || exponent > 400 {
					return result, reject("codemode option exponent out of range")
				}
			}
			number, ok := new(big.Rat).SetString(rawNumber)
			if !ok || !number.IsInt() || !number.Num().IsInt64() {
				return result, reject("codemode option must be a safe integer")
			}
			integer := number.Num().Int64()
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
