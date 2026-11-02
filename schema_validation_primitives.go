package goai

// Conversion ordering follows pinned pi-schema/TypeBox rules (MIT), Copyright
// (c) 2017-2026 Haydn Paterson. See THIRD_PARTY_NOTICES.md.
import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var schemaDecimalNumberPattern = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)
var schemaBigIntNumberPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)n$`)

// Scalar types first use TypeBox's conversion rules. Type arrays skip this
// pass and retain primitive/Ajv rules. Matching native values remain untouched.
func convertJSONSchemaPrimitive(value interface{}, kind string) (interface{}, error) {
	switch kind {
	case "string":
		if value == nil {
			return "null", nil
		}
	case "boolean":
		if value == nil {
			return false, nil
		}
		if text, ok := value.(string); ok {
			switch schemaFormatASCIIToLower(text) {
			case "true", "1":
				return true, nil
			case "false", "0":
				return false, nil
			}
		}
	case "null":
		if text, ok := value.(string); ok {
			switch schemaFormatASCIIToLower(text) {
			case "null", "undefined", "0", "":
				return nil, nil
			}
		}
	case "number", "integer":
		if exact, ok := value.(json.Number); ok && kind == "integer" {
			converted, _, err := schemaTruncateDecimal(exact)
			return converted, err
		}
		if text, ok := value.(string); ok {
			if number, valid := schemaParseNumber(text, true); valid {
				if kind == "integer" {
					return schemaIntegerValue(math.Trunc(number)), nil
				}
				return number, nil
			}
			switch schemaFormatASCIIToLower(text) {
			case "true":
				if kind == "integer" {
					return schemaIntegerValue(1), nil
				}
				return float64(1), nil
			case "false":
				if kind == "integer" {
					return schemaIntegerValue(0), nil
				}
				return float64(0), nil
			}
			if schemaBigIntNumberPattern.MatchString(text) {
				if number, err := strconv.ParseInt(text[:len(text)-1], 10, 64); err == nil && number >= -9007199254740991 && number <= 9007199254740991 {
					if kind == "integer" {
						return schemaIntegerValue(float64(number)), nil
					}
					return float64(number), nil
				}
			}
			return nil, fmt.Errorf("expected %s", kind)
		}
		if number, ok := value.(float64); ok && kind == "integer" && !math.IsNaN(number) && !math.IsInf(number, 0) {
			return schemaIntegerValue(math.Trunc(number)), nil
		}
	}
	return coerceJSONSchemaType(value, kind)
}

func schemaIntegerValue(value float64) interface{} {
	// Keep negative zero and avoid overflowing int at its rounded upper edge.
	if value == 0 && math.Signbit(value) || value >= float64(math.MaxInt) || value < float64(math.MinInt) {
		return value
	}
	return int(value)
}

// ECMAScript StringNumericLiteral: no Go hex floats, underscores or non-finite
// spellings. Non-decimal integer literals may exceed uint64; conversion stays
// bounded to the largest finite double, with no huge temporary integer.
func schemaParseNumber(value string, empty bool) (float64, bool) {
	text := strings.TrimFunc(value, schemaNumberWhitespace)
	if text == "" {
		return 0, empty
	}
	if len(text) >= 2 && text[0] == '0' {
		base, maxDigits := 0, 0
		switch text[1] {
		case 'x', 'X':
			base, maxDigits = 16, 256
		case 'o', 'O':
			base, maxDigits = 8, 342
		case 'b', 'B':
			base, maxDigits = 2, 1024
		}
		if base != 0 {
			digits := strings.TrimLeft(text[2:], "0")
			if len(text) == 2 {
				return 0, false
			}
			if digits == "" {
				return 0, true
			}
			if len(digits) > maxDigits {
				return 0, false
			}
			for _, c := range digits {
				digit := -1
				switch {
				case c >= '0' && c <= '9':
					digit = int(c - '0')
				case c >= 'a' && c <= 'f':
					digit = int(c-'a') + 10
				case c >= 'A' && c <= 'F':
					digit = int(c-'A') + 10
				}
				if digit < 0 || digit >= base {
					return 0, false
				}
			}
			n, ok := new(big.Int).SetString(digits, base)
			if !ok {
				return 0, false
			}
			number, _ := n.Float64()
			return number, !math.IsInf(number, 0)
		}
	}
	if !schemaDecimalNumberPattern.MatchString(text) {
		return 0, false
	}
	number, err := strconv.ParseFloat(text, 64)
	return number, err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
}

func schemaNumberWhitespace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xa0, 0x1680, 0x2000, 0x2001, 0x2002, 0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200a, 0x2028, 0x2029, 0x202f, 0x205f, 0x3000, 0xfeff:
		return true
	}
	return false
}

// Exact native decimal integer membership/truncation. No float64 rounding or
// exponent-sized expansion: an already-integral spelling stays unchanged.
func schemaTruncateDecimal(value json.Number) (json.Number, bool, error) {
	canonical, err := schemaDecimalString(value)
	if err != nil {
		return "", false, err
	}
	sign := ""
	if strings.HasPrefix(canonical, "-") {
		sign, canonical = "-", canonical[1:]
	}
	mantissa, point := canonical, int64(len(canonical))
	if e := strings.IndexByte(canonical, 'e'); e >= 0 {
		mantissa = canonical[:e]
		exponent, err := strconv.ParseInt(canonical[e+1:], 10, 64)
		if err != nil {
			return "", false, err
		}
		point = exponent + 1
	} else if dot := strings.IndexByte(canonical, '.'); dot >= 0 {
		point = int64(dot)
	}
	digits := strings.ReplaceAll(mantissa, ".", "")
	if point >= int64(len(digits)) {
		return value, true, nil
	}
	if point <= 0 {
		return json.Number(sign + "0"), false, nil
	}
	return json.Number(sign + digits[:int(point)]), false, nil
}
