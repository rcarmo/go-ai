package durable

import (
	"reflect"

	goai "github.com/rcarmo/go-ai"
)

var toolSamplingType = reflect.TypeFor[goai.ToolConstrainedSampling]()
var toolSamplingPointerType = reflect.TypeFor[*goai.ToolConstrainedSampling]()

// Project only this exact library type without invoking its marshal method.
// The bounded encoder still walks the ordinary fields and limits every value.
func reflectedToolSampling(v reflect.Value) (any, bool, error) {
	if !v.IsValid() {
		return nil, false, nil
	}
	for v.Kind() == reflect.Interface && !v.IsNil() {
		v = v.Elem()
	}
	if v.Type() == toolSamplingPointerType {
		if v.IsNil() {
			return nil, true, nil
		}
		v = v.Elem()
	}
	if v.Type() != toolSamplingType || !v.CanInterface() {
		return nil, false, nil
	}
	sample := v.Interface().(goai.ToolConstrainedSampling)
	if sample.Type == goai.ToolConstrainedSamplingDisabled {
		if sample.Strict != "" || len(sample.Variants) != 0 {
			return nil, true, reject("disabled tool constraints cannot specify a schema or grammar")
		}
		return false, true, nil
	}
	type fields goai.ToolConstrainedSampling
	return fields(sample), true, nil
}

func copyToolConstraints(value *goai.ToolConstrainedSampling, l Limits) (*goai.ToolConstrainedSampling, error) {
	if value == nil {
		return nil, nil
	}
	if value.Strict != "" && value.Strict != "prefer" && value.Strict != "require" {
		return nil, reject("invalid tool constraint strictness")
	}
	switch value.Type {
	case goai.ToolConstrainedSamplingDisabled:
		if value.Strict != "" || len(value.Variants) != 0 {
			return nil, reject("disabled tool constraints cannot specify a schema or grammar")
		}
		return &goai.ToolConstrainedSampling{Type: goai.ToolConstrainedSamplingDisabled}, nil
	case "json_schema":
		if len(value.Variants) != 0 {
			return nil, reject("schema constraints cannot have grammar variants")
		}
	case "grammar":
		if value.Strict != "" || len(value.Variants) == 0 || len(value.Variants) > 2 {
			return nil, reject("invalid tool grammar constraints")
		}
		for name, source := range value.Variants {
			if name != "openai_lark" && name != "openai_regex" || source == "" || len(source) > l.MaxStringBytes {
				return nil, reject("invalid tool grammar variant")
			}
		}
	default:
		return nil, reject("invalid tool constraint type")
	}
	copy := *value
	if value.Variants != nil {
		copy.Variants = map[string]string{}
		for key, source := range value.Variants {
			copy.Variants[key] = source
		}
	}
	if _, err := dtoObject(copy, l); err != nil {
		return nil, err
	}
	return &copy, nil
}

func sameToolConstraints(a, b *goai.ToolConstrainedSampling) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Type != b.Type || a.Strict != b.Strict || len(a.Variants) != len(b.Variants) {
		return false
	}
	for key, value := range a.Variants {
		if b.Variants[key] != value {
			return false
		}
	}
	return true
}
