package goai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/rcarmo/go-ai/internal/errortext"
	"github.com/rcarmo/go-ai/internal/jsonparse"
)

const MaxProviderErrorBodyChars = 4000

type NormalizedProviderError struct {
	Status             int
	HasStatus          bool
	Body               string
	HasBody            bool
	Message            string
	MessageCarriesBody bool
}

type ProviderErrorShape interface {
	error
	ProviderErrorStatus() (int, bool)
	ProviderErrorBody() (any, bool)
}

func NormalizeProviderError(err any) NormalizedProviderError {
	if err == nil {
		return NormalizedProviderError{Message: "null"}
	}
	if pe, ok := err.(ProviderErrorShape); ok {
		status, hasStatus := pe.ProviderErrorStatus()
		body, hasBody := normalizeProviderErrorBody(pe.ProviderErrorBody())
		message := pe.Error()
		carries := !hasBody || strings.Contains(message, body)
		return NormalizedProviderError{Status: status, HasStatus: hasStatus, Body: body, HasBody: hasBody, Message: message, MessageCarriesBody: carries}
	}
	if e, ok := err.(error); ok {
		return NormalizedProviderError{Message: e.Error(), MessageCarriesBody: true}
	}
	return NormalizedProviderError{Message: SafeJSONStringify(err)}
}

func normalizeProviderErrorBody(body any, ok bool) (string, bool) {
	if !ok || body == nil {
		return "", false
	}
	var text string
	switch v := body.(type) {
	case string:
		text = v
	default:
		// Unnamed maps with built-in string keys are native plain JSON objects.
		// Named host wrappers, arrays, scalars and streams are not error bodies.
		value := reflect.ValueOf(v)
		if value.Kind() != reflect.Map || value.Type().Name() != "" || value.Type().Key() != reflect.TypeOf("") || value.Len() == 0 {
			return "", false
		}
		text = SafeJSONStringify(v)
	}
	text = errortext.Trim(text)
	if text == "" {
		return "", false
	}
	return TruncateErrorText(text, MaxProviderErrorBodyChars), true
}

func FormatProviderError(norm NormalizedProviderError, prefix ...string) string {
	p := ""
	if len(prefix) > 0 {
		p = prefix[0]
	}
	if norm.MessageCarriesBody || !norm.HasStatus || !norm.HasBody {
		if p != "" && norm.HasStatus {
			return fmt.Sprintf("%s (%d): %s", p, norm.Status, norm.Message)
		}
		return norm.Message
	}
	if p != "" {
		return fmt.Sprintf("%s (%d): %s", p, norm.Status, norm.Body)
	}
	return fmt.Sprintf("%d: %s", norm.Status, norm.Body)
}

// TruncateErrorText counts UTF-16 units, retaining complete Unicode scalars.
// A split surrogate-pair boundary rounds down and counts both omitted units.
// As before, maxChars must be non-negative.
func TruncateErrorText(text string, maxChars int) string {
	if maxChars < 0 {
		panic("negative provider error text limit")
	}
	if len(text) <= maxChars {
		return text
	}
	total, kept, end := 0, 0, len(text)
	cut := false
	for offset, char := range text {
		units := 1
		if char > 0xffff {
			units = 2
		}
		if !cut && total+units > maxChars {
			end, kept, cut = offset, total, true
		}
		total += units
	}
	if !cut {
		return text
	}
	return fmt.Sprintf("%s... [truncated %d chars]", text[:end], total-kept)
}

func SafeJSONStringify(value any) string {
	b, ok := tryErrorJSON(value)
	if !ok {
		// JS JSON.stringify normalizes nonfinite parser-owned values to null.
		if encoded, encodeErr := jsonparse.MarshalParsedJSONValue(value); encodeErr == nil {
			return string(normalizeErrorJSONNumbers(encoded))
		}
		// fmt.Sprint recursively traverses cyclic containers. Use bounded JS
		// string coercion instead of turning a diagnostic into a stack overflow.
		return errorStringFallback(value, make(map[errorArrayVisit]bool), 0)
	}
	return string(b)
}

func tryErrorJSON(value any) (encoded []byte, ok bool) {
	defer func() {
		if recover() != nil {
			encoded = nil
			ok = false
		}
	}()
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, false
	}
	// Encode adds a record delimiter; it is not part of JSON.stringify text.
	encoded = bytes.TrimSuffix(buffer.Bytes(), []byte{'\n'})
	return normalizeErrorJSONNumbers(errorJSONSeparators(encoded)), true
}

// Go escapes these JS line separators even with EscapeHTML disabled. Preserve
// their source text while leaving literal backslash-u sequences untouched.
func errorJSONSeparators(encoded []byte) []byte {
	if !bytes.Contains(encoded, []byte(`\u202`)) {
		return encoded
	}
	var out bytes.Buffer
	for i := 0; i < len(encoded); {
		if encoded[i] != '\\' {
			out.WriteByte(encoded[i])
			i++
			continue
		}
		if i+6 <= len(encoded) && (bytes.Equal(encoded[i:i+6], []byte(`\u2028`)) || bytes.Equal(encoded[i:i+6], []byte(`\u2029`))) {
			if encoded[i+5] == '8' {
				out.WriteString("\u2028")
			} else {
				out.WriteString("\u2029")
			}
			i += 6
			continue
		}
		out.WriteByte(encoded[i])
		i++
		if i < len(encoded) {
			out.WriteByte(encoded[i])
			i++
		}
	}
	return out.Bytes()
}

type errorArrayVisit struct {
	kind    reflect.Type
	pointer uintptr
	length  int
}

func errorStringFallback(value any, active map[errorArrayVisit]bool, depth int) string {
	if value == nil {
		return "null"
	}
	if depth >= 128 {
		return ""
	}
	// Explicit host coercion keeps the existing Go Stringer/error behaviour.
	if _, ok := value.(fmt.Stringer); ok {
		return fmt.Sprint(value)
	}
	if _, ok := value.(error); ok {
		return fmt.Sprint(value)
	}
	if number, ok := value.(float64); ok {
		if math.IsInf(number, 1) {
			return "Infinity"
		}
		if math.IsInf(number, -1) {
			return "-Infinity"
		}
		if math.IsNaN(number) {
			return "NaN"
		}
		if number == 0 {
			return "0"
		}
		encoded, _ := json.Marshal(number)
		return string(encoded)
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Map, reflect.Struct:
		return "[object Object]"
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return "null"
		}
		return errorStringFallback(v.Elem().Interface(), active, depth+1)
	case reflect.Slice, reflect.Array:
		if v.Kind() == reflect.Slice {
			visit := errorArrayVisit{v.Type(), v.Pointer(), v.Len()}
			if active[visit] {
				return ""
			}
			active[visit] = true
			defer delete(active, visit)
		}
		parts := make([]string, v.Len())
		for i := range parts {
			element := v.Index(i).Interface()
			if element != nil {
				reflected := reflect.ValueOf(element)
				switch reflected.Kind() {
				case reflect.Map, reflect.Slice, reflect.Pointer, reflect.Interface:
					if reflected.IsNil() {
						continue
					}
				}
				parts[i] = errorStringFallback(element, active, depth+1)
			}
		}
		return strings.Join(parts, ",")
	default:
		return fmt.Sprint(value)
	}
}
