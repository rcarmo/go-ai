package goai

import "reflect"

type toolSchemaPropertyAnnotations struct {
	all   bool
	names map[string]bool
}

type toolSchemaPropertyAnnotationVisit struct {
	schema uintptr
	scope  string
}

func (v *toolSchemaValidator) firstUnevaluatedPropertyFailure(object, schema map[string]interface{}) (string, bool) {
	raw, present := schema["unevaluatedProperties"]
	if v.draft7 || !present || raw == true || len(object) == 0 {
		return "", false
	}
	annotations := toolSchemaPropertyAnnotations{names: make(map[string]bool)}
	v.collectEvaluatedProperties(object, schema, &annotations, make(map[toolSchemaPropertyAnnotationVisit]bool), true)
	if !annotations.all {
		for _, key := range sortedSchemaKeys(object) {
			if !annotations.names[key] && !v.acceptsRaw(object[key], raw) {
				return key, true
			}
		}
	}
	return "", false
}

// Annotations belong to the current object instance and dynamic resource stack.
// A passing child with unevaluatedProperties has checked all names left by its
// own annotations. It contributes all names, but cannot read its siblings'.
func (v *toolSchemaValidator) collectEvaluatedProperties(object, schema map[string]interface{}, out *toolSchemaPropertyAnnotations, seen map[toolSchemaPropertyAnnotationVisit]bool, own bool) {
	if out.all {
		return
	}
	if v.trackDynamic {
		defer v.enterSchemaResource(schema)()
	}
	visit := toolSchemaPropertyAnnotationVisit{reflect.ValueOf(schema).Pointer(), v.scopeKey}
	if seen[visit] {
		return
	}
	seen[visit] = true

	members := v.membersFor(schema)
	_, unevaluated := schema["unevaluatedProperties"]
	if members.additional != nil || unevaluated && !own {
		out.all = true
		return
	}
	for key := range object {
		if out.names[key] {
			continue
		}
		if _, declared := members.properties[key]; declared {
			out.names[key] = true
			continue
		}
		for _, pattern := range members.patterns {
			if v.matchSchemaPattern(pattern.source, key).matched {
				out.names[key] = true
				break
			}
		}
	}
	add := func(raw interface{}, knownPassing bool) {
		if child, ok := raw.(map[string]interface{}); ok && !out.all &&
			(knownPassing || v.checkReferenceTarget(object, child, "")) {
			v.collectEvaluatedProperties(object, child, out, seen, false)
		}
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		if branches, ok := schema[keyword].([]interface{}); ok {
			for _, branch := range branches {
				add(branch, keyword == "allOf")
			}
		}
	}
	if condition, present := schema["if"]; present {
		if v.acceptsRaw(object, condition) {
			add(condition, true)
			add(schema["then"], true)
		} else {
			add(schema["else"], true)
		}
	}
	for _, keyword := range []string{"$ref", "$dynamicRef"} {
		dynamic := keyword == "$dynamicRef"
		ref, ok := schema[keyword].(string)
		if !ok || dynamic && !v.trackDynamic || out.all {
			continue
		}
		target, base := v.referenceTarget(ref, dynamic, schema)
		if child, ok := target.(map[string]interface{}); ok && v.checkReferenceTarget(object, child, base) {
			leave := func() {}
			if base != "" {
				leave = v.enterSchemaScope(base)
			}
			v.collectEvaluatedProperties(object, child, out, seen, false)
			leave()
		}
	}
	for _, keyword := range []string{"dependentSchemas", "dependencies"} {
		if entries, ok := schema[keyword].(map[string]interface{}); ok {
			for key, entry := range entries {
				if _, present := object[key]; present {
					add(entry, false)
				}
			}
		}
	}
}
