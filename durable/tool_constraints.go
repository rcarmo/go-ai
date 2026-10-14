package durable

import goai "github.com/rcarmo/go-ai"

func copyToolConstraints(value *goai.ToolConstrainedSampling, l Limits) (*goai.ToolConstrainedSampling, error) {
	if value == nil {
		return nil, nil
	}
	if value.Strict != "" && value.Strict != "prefer" && value.Strict != "require" {
		return nil, reject("invalid tool constraint strictness")
	}
	switch value.Type {
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
