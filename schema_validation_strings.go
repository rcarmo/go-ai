package goai

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// JSON's float writer uses ECMAScript's fixed/exponential thresholds and shortest
// round-tripping decimal. String(number) additionally keeps non-finite names and
// removes the sign of zero; neither operation changes the caller's number.
func schemaFloatString(value float64) string {
	switch {
	case value == 0:
		return "0"
	case math.IsNaN(value):
		return "NaN"
	case math.IsInf(value, 1):
		return "Infinity"
	case math.IsInf(value, -1):
		return "-Infinity"
	default:
		encoded, _ := json.Marshal(value) // Finite float64 always encodes.
		return string(encoded)
	}
}

// json.Number is a native exact-decimal extension, not a JS double. Apply the
// same notation thresholds without rounding its digits through float64. Work
// and output size stay proportional to the supplied spelling, not its exponent.
func schemaDecimalString(value json.Number) (string, error) {
	if value == "" {
		return "", fmt.Errorf("empty decimal number")
	}
	if _, err := json.Marshal(value); err != nil { // Validate JSON number grammar.
		return "", err
	}
	text := value.String()
	sign := ""
	if text[0] == '-' {
		sign, text = "-", text[1:]
	}
	mantissa, exponentText := text, "0"
	if i := strings.IndexAny(text, "eE"); i >= 0 {
		mantissa, exponentText = text[:i], text[i+1:]
	}
	point := int64(len(mantissa))
	if dot := strings.IndexByte(mantissa, '.'); dot >= 0 {
		point = int64(dot)
		mantissa = mantissa[:dot] + mantissa[dot+1:]
	}
	leading := len(mantissa) - len(strings.TrimLeft(mantissa, "0"))
	if leading == len(mantissa) {
		return "0", nil
	}
	digits := strings.TrimRight(mantissa[leading:], "0")
	point -= int64(leading)
	exponent, err := strconv.ParseInt(exponentText, 10, 64)
	if err != nil {
		return "", err
	}
	if exponent > 0 && point > math.MaxInt64-exponent || exponent < 0 && point < math.MinInt64-exponent {
		return "", fmt.Errorf("decimal exponent out of range")
	}
	point += exponent
	switch {
	case point > 0 && point <= 21:
		if int64(len(digits)) <= point {
			return sign + digits + strings.Repeat("0", int(point)-len(digits)), nil
		}
		return sign + digits[:int(point)] + "." + digits[int(point):], nil
	case point <= 0 && point > -6:
		return sign + "0." + strings.Repeat("0", int(-point)) + digits, nil
	default:
		if point == math.MinInt64 {
			return "", fmt.Errorf("decimal exponent out of range")
		}
		coefficient := digits[:1]
		if len(digits) > 1 {
			coefficient += "." + digits[1:]
		}
		exponentSign := ""
		if point > 0 {
			exponentSign = "+"
		}
		return sign + coefficient + "e" + exponentSign + strconv.FormatInt(point-1, 10), nil
	}
}
