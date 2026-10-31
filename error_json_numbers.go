package goai

import (
	"bytes"
	"strconv"
	"strings"
)

// normalizeErrorJSONNumbers changes diagnostic spelling, not numeric precision.
// Work on already validated JSON, never on the contents of a quoted string.
func normalizeErrorJSONNumbers(encoded []byte) []byte {
	if !bytes.ContainsAny(encoded, "-eE") {
		return encoded
	}
	var out bytes.Buffer
	last := 0
	for i := 0; i < len(encoded); {
		start := i
		if encoded[i] == '"' {
			i++
			for i < len(encoded) {
				if encoded[i] == '\\' {
					i += 2
					continue
				}
				if encoded[i] == '"' {
					i++
					break
				}
				i++
			}
			continue
		}
		if encoded[i] == '-' || encoded[i] >= '0' && encoded[i] <= '9' {
			for i < len(encoded) && strings.IndexByte(",]} \t\r\n", encoded[i]) < 0 {
				i++
			}
			token := string(encoded[start:i])
			normalized := errorJSONNumber(token)
			if normalized != token {
				out.Write(encoded[last:start])
				out.WriteString(normalized)
				last = i
			}
			continue
		}
		i++
	}
	if last == 0 {
		return encoded
	}
	out.Write(encoded[last:])
	return out.Bytes()
}

func errorJSONNumber(token string) string {
	if token == "-0" {
		return "0"
	}
	split := strings.IndexAny(token, "eE")
	if split < 0 {
		return token
	}
	exponent, err := strconv.ParseInt(token[split+1:], 10, 32)
	if err != nil {
		return token
	} // Keep native exact numbers outside float64's range.
	mantissa := token[:split]
	sign := ""
	if strings.HasPrefix(mantissa, "-") {
		sign = "-"
		mantissa = mantissa[1:]
	}
	point := strings.IndexByte(mantissa, '.')
	if point < 0 {
		point = len(mantissa)
	}
	digits := strings.ReplaceAll(mantissa, ".", "")
	leading := len(digits) - len(strings.TrimLeft(digits, "0"))
	if leading == len(digits) {
		return "0"
	}
	exponent += int64(point - leading - 1)
	digits = strings.TrimRight(digits[leading:], "0")
	if exponent >= 21 || exponent < -6 {
		fraction := ""
		if len(digits) > 1 {
			fraction = "." + digits[1:]
		}
		exponentSign := ""
		if exponent >= 0 {
			exponentSign = "+"
		}
		return sign + digits[:1] + fraction + "e" + exponentSign + strconv.FormatInt(exponent, 10)
	}
	point = int(exponent) + 1
	if point <= 0 {
		return sign + "0." + strings.Repeat("0", -point) + digits
	}
	if point >= len(digits) {
		return sign + digits + strings.Repeat("0", point-len(digits))
	}
	return sign + digits[:point] + "." + digits[point:]
}
