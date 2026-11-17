package goai

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// A JSON-shaped structured clone must also detach descendants that have no
// schema constraints. Keep numeric/string values intact and memoise containers
// so a cyclic caller graph cannot exhaust the Go stack during ownership copy.
func cloneToolArgumentValue(value any) (any, error) {
	type identity struct {
		typeOf  reflect.Type
		pointer uintptr
		length  int
	}
	seen := map[identity]any{}
	var clone func(reflect.Value) (any, error)
	clone = func(value reflect.Value) (any, error) {
		if !value.IsValid() {
			return nil, nil
		}
		for value.Kind() == reflect.Interface {
			if value.IsNil() {
				return nil, nil
			}
			value = value.Elem()
		}
		switch scalar := value.Interface().(type) {
		case UTF16String:
			return scalar, nil
		case json.Number:
			return scalar, nil
		}
		switch value.Kind() {
		case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Float32, reflect.Float64:
			return value.Interface(), nil
		case reflect.Map:
			if value.Type().Key().Kind() != reflect.String {
				return nil, fmt.Errorf("tool argument object keys must be strings")
			}
			if value.IsNil() {
				return map[string]any(nil), nil
			}
			key := identity{typeOf: value.Type(), pointer: value.Pointer()}
			if previous, ok := seen[key]; ok {
				return previous, nil
			}
			out := make(map[string]any, value.Len())
			seen[key] = out
			iterator := value.MapRange()
			for iterator.Next() {
				child, err := clone(iterator.Value())
				if err != nil {
					return nil, err
				}
				out[iterator.Key().String()] = child
			}
			return out, nil
		case reflect.Array, reflect.Slice:
			if value.Kind() == reflect.Slice && value.IsNil() {
				return []any(nil), nil
			}
			var key identity
			if value.Kind() == reflect.Slice {
				key = identity{value.Type(), value.Pointer(), value.Len()}
				if previous, ok := seen[key]; ok {
					return previous, nil
				}
			}
			out := make([]any, value.Len())
			if value.Kind() == reflect.Slice {
				seen[key] = out
			}
			for index := range out {
				child, err := clone(value.Index(index))
				if err != nil {
					return nil, err
				}
				out[index] = child
			}
			return out, nil
		default:
			return nil, fmt.Errorf("tool argument value cannot be cloned: %s", value.Type())
		}
	}
	return clone(reflect.ValueOf(value))
}
