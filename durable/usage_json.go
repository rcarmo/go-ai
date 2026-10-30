package durable

import (
	"reflect"

	goai "github.com/rcarmo/go-ai"
)

var nativeUsageType = reflect.TypeFor[goai.Usage]()

func isNativeUsageType(t reflect.Type) bool {
	return t == nativeUsageType || t.Kind() == reflect.Pointer && t.Elem() == nativeUsageType
}

// The bounded encoder handles this exact library type itself; it never calls
// its marshaler (and never grants the exception to caller-defined wrappers).
func usageZeroIsPresent(v reflect.Value, name string) bool {
	if v.Type() != nativeUsageType {
		return false
	}
	switch name {
	case "cacheWrite1h":
		return v.FieldByName("CacheWrite1hSet").Bool()
	case "reasoning":
		return v.FieldByName("ReasoningSet").Bool()
	}
	return false
}

// Usage's public JSON decoder tolerates unknown fields like ordinary Go JSON.
// Restore strict DTO rejection, including nested cost fields, after decoding.
// The walk also covers pre-populated interface targets and typed containers.
func rejectUnknownUsageFields(target any) error {
	type visit struct {
		typ reflect.Type
		ptr uintptr
	}
	seen := map[visit]bool{}
	var walk func(reflect.Value) error
	walk = func(v reflect.Value) error {
		if !v.IsValid() {
			return nil
		}
		if v.Type() == nativeUsageType && v.CanInterface() {
			if v.Interface().(goai.Usage).HasUnknownJSONFields() {
				return reject("unknown usage JSON field")
			}
			return nil
		}
		switch v.Kind() {
		case reflect.Interface:
			if !v.IsNil() {
				return walk(v.Elem())
			}
		case reflect.Pointer:
			if v.IsNil() {
				return nil
			}
			p := visit{v.Type(), v.Pointer()}
			if seen[p] {
				return nil
			}
			seen[p] = true
			return walk(v.Elem())
		case reflect.Struct:
			for _, field := range structJSONFields(v.Type()) {
				if err := walk(v.Field(field.index)); err != nil {
					return err
				}
			}
		case reflect.Map:
			if v.IsNil() {
				return nil
			}
			p := visit{v.Type(), v.Pointer()}
			if seen[p] {
				return nil
			}
			seen[p] = true
			it := v.MapRange()
			for it.Next() {
				if err := walk(it.Value()); err != nil {
					return err
				}
			}
		case reflect.Slice, reflect.Array:
			if v.Type().Elem().Kind() == reflect.Uint8 {
				return nil
			}
			if v.Kind() == reflect.Slice {
				if v.IsNil() {
					return nil
				}
				p := visit{v.Type(), v.Pointer()}
				if p.ptr != 0 && seen[p] {
					return nil
				}
				seen[p] = true
			}
			for i := 0; i < v.Len(); i++ {
				if err := walk(v.Index(i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(reflect.ValueOf(target))
}
