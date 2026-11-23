package goai

import "reflect"

// Like pi-schema's annotations.ts, track a contiguous prefix separately from
// the sparse matches of contains. The annotations belong to one array value;
// validation of an item never contributes that item's own nested annotations.
type toolSchemaItemAnnotations struct {
	prefix  int
	matches map[int]bool
}

type toolSchemaItemAnnotationVisit struct {
	schema uintptr
	scope  string
}

func (v *toolSchemaValidator) firstUnevaluatedItemFailure(array []interface{}, schema map[string]interface{}, enterRoot bool) int {
	first := -1
	v.visitUnevaluatedItemFailures(array, schema, enterRoot, func(index int, _ interface{}) bool {
		first = index
		return false
	})
	return first
}

// Both validation and explanation use the same annotations. Validation stops
// at the first failure; a standalone explanation can visit every failed index.
func (v *toolSchemaValidator) visitUnevaluatedItemFailures(array []interface{}, schema map[string]interface{}, enterRoot bool, yield func(int, interface{}) bool) {
	raw, present := schema["unevaluatedItems"]
	if v.draft7 || !present || raw == true || len(array) == 0 {
		return
	}
	var evaluated toolSchemaItemAnnotations
	v.collectEvaluatedItems(array, schema, &evaluated, make(map[toolSchemaItemAnnotationVisit]bool), true, enterRoot)
	for i := evaluated.prefix; i < len(array); i++ {
		if !evaluated.matches[i] && !v.acceptsRaw(array[i], raw) && !yield(i, raw) {
			return
		}
	}
}

// All contributors have passed at this location before their annotations are
// used. In particular, a passing child with unevaluatedItems has already
// checked the indexes that its own keywords did not evaluate.
func (v *toolSchemaValidator) collectEvaluatedItems(array []interface{}, schema map[string]interface{}, out *toolSchemaItemAnnotations, seen map[toolSchemaItemAnnotationVisit]bool, own, enterRoot bool) {
	if out.prefix == len(array) {
		return
	}
	if v.trackDynamic && (!own || enterRoot) {
		defer v.enterSchemaResource(schema)()
	}
	visit := toolSchemaItemAnnotationVisit{reflect.ValueOf(schema).Pointer(), v.scopeKey}
	if seen[visit] {
		return
	}
	seen[visit] = true

	prefix, _ := validationTupleItems(schema)
	rest := schema["items"]
	if _, legacy := rest.([]interface{}); legacy {
		rest = schema["additionalItems"]
	}
	_, unevaluated := schema["unevaluatedItems"]
	if rest != nil || unevaluated && !own {
		out.prefix = len(array)
		return
	}
	out.prefix = max(out.prefix, min(len(prefix), len(array)))
	if contains, exists := schema["contains"]; exists && out.prefix < len(array) {
		for i := out.prefix; i < len(array); i++ {
			if !out.matches[i] && v.acceptsRaw(array[i], contains) {
				if out.matches == nil {
					out.matches = make(map[int]bool)
				}
				out.matches[i] = true
			}
		}
	}
	add := func(raw interface{}, knownPassing bool) {
		if child, ok := raw.(map[string]interface{}); ok && out.prefix < len(array) &&
			(knownPassing || v.checkReferenceTarget(array, child, "")) {
			v.collectEvaluatedItems(array, child, out, seen, false, enterRoot)
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
		if v.acceptsRaw(array, condition) {
			add(condition, true)
			add(schema["then"], true)
		} else {
			add(schema["else"], true)
		}
	}
	for _, keyword := range []string{"$ref", "$dynamicRef"} {
		dynamic := keyword == "$dynamicRef"
		ref, ok := schema[keyword].(string)
		if !ok || dynamic && !v.trackDynamic || out.prefix == len(array) {
			continue
		}
		target, base := v.referenceTarget(ref, dynamic, schema)
		if child, ok := target.(map[string]interface{}); ok && v.checkReferenceTarget(array, child, base) {
			leave := func() {}
			if base != "" {
				leave = v.enterSchemaScope(base)
			}
			v.collectEvaluatedItems(array, child, out, seen, false, enterRoot)
			leave()
		}
	}
}
