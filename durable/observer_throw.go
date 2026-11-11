package durable

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// guardObserver reports the original throw, then toError separately converts a
// non-Error into an Error(String(value)). No ownership copy belongs here: the
// report must retain the exact thrown object, including cyclic host values.
func commitObserverEndError(err error) error {
	var failure *Failure
	if !errors.As(err, &failure) {
		return err
	}
	return errors.New(observerThrowMessage(failure.Value()))
}

// A host Stringer represents the source object's toString method. A failed
// conversion falls back once for the original throw, as source toError does;
// catching at each nested array element would produce a different message.
func observerThrowMessage(value any) (message string) {
	defer func() {
		if recover() != nil {
			message = observerThrowTag(value)
		}
	}()
	return observerThrowString(value, make(map[observerThrowArray]bool))
}

type observerThrowArray struct {
	pointer uintptr
	length  int
	kind    reflect.Type
}

func observerThrowString(value any, active map[observerThrowArray]bool) string {
	if value == nil {
		return "null"
	}
	// json.Number.String preserves its wire spelling; source String(number)
	// instead uses the Number value, including exponent thresholds and -0.
	if number, ok := value.(json.Number); ok {
		parsed, err := number.Float64()
		if err == nil || math.IsInf(parsed, 0) {
			return observerThrowNumber(parsed)
		}
		return number.String()
	}
	if err, ok := value.(error); ok {
		return "Error: " + err.Error()
	}
	if stringer, ok := value.(fmt.Stringer); ok {
		return stringer.String()
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.String:
		return v.String()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return observerThrowNumber(float64(v.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return observerThrowNumber(float64(v.Uint()))
	case reflect.Float32, reflect.Float64:
		return observerThrowNumber(v.Float())
	case reflect.Slice, reflect.Array:
		// Array.prototype.join emits an empty field for null and for an array
		// already on the conversion stack. Shared, noncyclic arrays convert again.
		if v.Len() == 0 {
			return ""
		}
		if v.Kind() == reflect.Slice {
			key := observerThrowArray{v.Pointer(), v.Len(), v.Type()}
			if active[key] {
				return ""
			}
			active[key] = true
			defer delete(active, key)
		}
		var out strings.Builder
		for i := 0; i < v.Len(); i++ {
			if i > 0 {
				out.WriteByte(',')
			}
			element := v.Index(i).Interface()
			if element != nil {
				out.WriteString(observerThrowString(element, active))
			}
		}
		return out.String()
	default:
		return "[object Object]"
	}
}
func observerThrowTag(value any) string {
	if value != nil {
		switch reflect.TypeOf(value).Kind() {
		case reflect.Slice, reflect.Array:
			return "[object Array]"
		}
	}
	return "[object Object]"
}
func observerThrowNumber(value float64) string {
	switch {
	case value == 0:
		return "0"
	case math.IsNaN(value):
		return "NaN"
	case math.IsInf(value, 1):
		return "Infinity"
	case math.IsInf(value, -1):
		return "-Infinity"
	}
	// encoding/json's float64 encoder uses the ECMAScript fixed/exponent
	// thresholds and removes leading exponent zeroes. Only a primitive float is
	// passed, so no caller marshaler can execute here.
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
