package goai

import (
	"bytes"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

func (v *toolSchemaValidator) enableDynamicReferences() {
	if raw := v.schemaRaw; len(raw) > 0 && !bytes.Contains(raw, []byte("$dynamicRef")) && !bytes.Contains(raw, []byte(`\u`)) {
		return
	}
	v.trackDynamic = v.resourceIndex().hasDynamicRef
}

func (v *toolSchemaValidator) enterSchemaResource(schema map[string]interface{}) func() {
	r := v.resourceIndex()
	id := reflect.ValueOf(schema).Pointer()
	base := r.bases[id]
	if root, ok := r.documents[base].(map[string]interface{}); ok && reflect.ValueOf(root).Pointer() == id {
		return v.enterSchemaScope(base)
	}
	return func() {}
}

func (v *toolSchemaValidator) enterSchemaScope(base string) func() {
	// The first match wins. Duplicating a URI on recursive visits cannot change
	// resolution, but would prevent the memo key from cutting reference cycles.
	if slices.Contains(v.scope, base) {
		return func() {}
	}
	before := v.scopeKey
	v.scope = append(v.scope, base)
	v.scopeKey += strconv.Itoa(len(base)) + ":" + base
	return func() { v.scope = v.scope[:len(v.scope)-1]; v.scopeKey = before }
}

func (v *toolSchemaValidator) checkDynamicReference(value interface{}, schema map[string]interface{}) bool {
	if !v.trackDynamic {
		return true
	}
	if ref, ok := schema["$dynamicRef"].(string); ok {
		target, base := v.referenceTarget(ref, true, schema)
		return v.checkReferenceTarget(value, target, base)
	}
	return true
}

// A dynamic ref is bookended only when its named fragment initially resolves
// to a matching dynamic anchor. Pointers and ordinary anchors stay static.
func (v *toolSchemaValidator) referenceTarget(ref string, dynamic bool, from ...map[string]interface{}) (interface{}, string) {
	if !v.trackDynamic {
		return v.resolvePureDataReference(ref, from...), ""
	}
	r := v.resourceIndex()
	fromBase := r.bases[reflect.ValueOf(v.root).Pointer()]
	if len(from) > 0 {
		if b, ok := r.bases[reflect.ValueOf(from[0]).Pointer()]; ok {
			fromBase = b
		}
	}
	initial, base := v.resolveFromBase(ref, fromBase)
	if dynamic {
		_, fragment, named := strings.Cut(resolveSchemaURI(ref, fromBase), "#")
		if target, ok := initial.(map[string]interface{}); ok && named && fragment != "" && !strings.HasPrefix(fragment, "/") && target["$dynamicAnchor"] == fragment {
			for _, uri := range v.scope {
				if candidate := r.dynamicAnchors[uri+"#"+fragment]; candidate != nil {
					return candidate, ""
				}
			}
			return initial, ""
		}
	}
	if base == fromBase {
		base = ""
	}
	return initial, base
}
