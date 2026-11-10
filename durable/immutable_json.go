package durable

import (
	"reflect"
	"strconv"
)

// immutableJSONValue recognises only this package's exact frozen type. Caller
// marshal methods, wrappers and look-alike structs gain no encoding authority.
func immutableJSONValue(v reflect.Value) (*ImmutableValue, bool) {
	if v.Type() == reflect.TypeFor[readonlyJSON]() {
		return v.Interface().(readonlyJSON).node, true
	}
	if v.Type() == reflect.TypeFor[*ImmutableValue]() {
		if v.IsNil() {
			return nil, true
		}
		return v.Interface().(*ImmutableValue), true
	}
	if v.Type() == reflect.TypeFor[ImmutableValue]() {
		value := v.Interface().(ImmutableValue)
		return &value, true
	}
	return nil, false
}

// immutable walks logical JSON nodes, not the private Go representation. The
// caller has already charged the root node, as for an ordinary map or slice.
func (b *jsonBudget) immutable(value *ImmutableValue, depth int) error {
	if value == nil {
		b.hint(4)
		return nil
	}
	switch value.kind {
	case "null":
		b.hint(4)
		return nil
	case "bool":
		b.hint(5)
		return nil
	case "string":
		return b.str(value.string)
	case "number":
		number := string(value.number)
		b.hint(len(number))
		if len(number) > b.l.MaxStringBytes {
			return reject("JSON numeric spelling byte limit")
		}
		return validNumber(number)
	case "object":
		if depth > b.l.MaxDepth || len(value.mapValue) > b.l.MaxMembers {
			return reject("JSON container limit")
		}
		b.hint(2 + 2*len(value.mapValue))
		for key, child := range value.mapValue {
			if err := b.node(); err != nil {
				return err
			}
			if err := b.key(key); err != nil {
				return err
			}
			if err := b.node(); err != nil {
				return err
			}
			if err := b.immutable(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	case "array":
		if depth > b.l.MaxDepth || len(value.arrayValue) > b.l.MaxMembers {
			return reject("JSON container limit")
		}
		b.hint(2 + len(value.arrayValue))
		for _, child := range value.arrayValue {
			if err := b.node(); err != nil {
				return err
			}
			if err := b.immutable(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return reject("unsupported immutable JSON kind")
	}
}

func encodeImmutableValue(b *limitedJSON, value *ImmutableValue) error {
	if value == nil {
		return emit(b, "null")
	}
	switch value.kind {
	case "null":
		return emit(b, "null")
	case "bool":
		return emit(b, strconv.FormatBool(value.bool))
	case "string":
		return encodeString(b, value.string)
	case "number":
		return emit(b, string(value.number))
	case "array":
		if err := emit(b, "["); err != nil {
			return err
		}
		for i, child := range value.arrayValue {
			if i > 0 {
				if err := emit(b, ","); err != nil {
					return err
				}
			}
			if err := encodeImmutableValue(b, child); err != nil {
				return err
			}
		}
		return emit(b, "]")
	case "object":
		if err := emit(b, "{"); err != nil {
			return err
		}
		if b.canonical && len(value.mapValue) > (b.max-len(b.data))/5 {
			return reject("encoded JSON byte limit")
		}
		first := true
		member := func(key string, child *ImmutableValue) error {
			if !first {
				if err := emit(b, ","); err != nil {
					return err
				}
			}
			first = false
			if err := encodeKey(b, key); err != nil {
				return err
			}
			if err := emit(b, ":"); err != nil {
				return err
			}
			return encodeImmutableValue(b, child)
		}
		if b.canonical {
			for _, key := range value.Keys() {
				if err := member(key, value.mapValue[key]); err != nil {
					return err
				}
			}
		} else {
			for key, child := range value.mapValue {
				if err := member(key, child); err != nil {
					return err
				}
			}
		}
		return emit(b, "}")
	default:
		return reject("unsupported immutable JSON kind")
	}
}

// MarshalJSON exposes only the frozen JSON value. Internal bounded encoders
// recognise the exact type directly and never invoke this method dynamically.
func (v ImmutableValue) MarshalJSON() ([]byte, error) {
	limits := maximumDurableLimits()
	return encodeBoundedOrder(v, limits, limits.MaxRecordBytes, true)
}
