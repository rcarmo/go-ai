package goai

// Resource indexing and URI fallback follow pi-schema c9f4c7b9 (MIT), Copyright
// (c) 2025 Mario Zechner. See THIRD_PARTY_NOTICES.md.
import (
	"bytes"
	"encoding/json"
	"net/url"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const toolSchemaDefaultBase = "https://json-schema.invalid/root.json"

type toolSchemaRefKey struct{ base, ref string }
type toolSchemaResources struct {
	bases          map[uintptr]string
	documents      map[string]interface{}
	external       map[string]toolSchemaExternalResource
	anchors        map[string]interface{}
	dynamicAnchors map[string]interface{}
	hasDynamicRef  bool
	orders         map[uintptr][]string
	compilerOrders map[uintptr][]string
	literalOrders  map[uintptr][]string
	work           *toolSchemaWork
	draft7         bool
}

// Index only schema-bearing keywords, in the pinned resolver's order. An ID
// inside enum, const, a default or an extension is data, not a registration.
var toolSchemaSingleKeywords = []string{"additionalProperties", "propertyNames", "contains", "not", "if", "then", "else", "unevaluatedProperties", "unevaluatedItems", "additionalItems", "contentSchema", "items"}
var toolSchemaMapKeywords = []string{"properties", "patternProperties", "$defs", "definitions", "dependentSchemas", "dependencies"}
var toolSchemaArrayKeywords = []string{"allOf", "anyOf", "oneOf", "prefixItems", "items"}

func (v *toolSchemaValidator) resourceIndex() *toolSchemaResources {
	if v.resources != nil {
		return v.resources
	}
	r := &toolSchemaResources{
		bases:     make(map[uintptr]string),
		documents: map[string]interface{}{toolSchemaDefaultBase: v.root},
		anchors:   make(map[string]interface{}),
		work:      v.work,
		draft7:    v.draft7,
	}
	v.resources = r
	if len(v.schemaRaw) != 0 {
		r.readOrder(v.root, v.schemaRaw)
	}
	r.index(v.root, toolSchemaDefaultBase)
	// Ordering metadata is needed only during this one indexing pass.
	r.orders = nil
	v.schemaRaw = nil
	return r
}

func schemaDocument(uri string) string {
	if i := strings.IndexByte(uri, '#'); i >= 0 {
		return uri[:i]
	}
	return uri
}

// Parse only: this never opens a URL or file. Preserve the source fallback for
// opaque/invalid bases when WHATWG resolution fails.
func resolveSchemaURI(ref, base string) string {
	if parsed, err := parseSchemaURL(base, ref); err == nil {
		uri := parsed.Href(false)
		// The pinned source follows the newer opaque-path rule: encode the
		// final space before a query/fragment. The parser predates that change.
		if parsed.OpaquePath() && strings.HasSuffix(parsed.Pathname(), " ") {
			last := len(parsed.Scheme()) + len(parsed.Pathname())
			return uri[:last] + "%20" + uri[last+1:]
		}
		return uri
	}
	if strings.HasPrefix(ref, "#") {
		return schemaDocument(base) + ref
	}
	return ref
}

func (r *toolSchemaResources) index(raw interface{}, base string) {
	node, ok := raw.(map[string]interface{})
	if !ok || node == nil {
		return
	}
	identity := reflect.ValueOf(node).Pointer()
	if _, seen := r.bases[identity]; seen {
		return
	}
	// In draft 7 the reference replaces the node, including any sibling ID
	// or subschemas. A fragment-only ID names an anchor, not a document.
	if _, ref := node["$ref"].(string); r.draft7 && ref {
		r.bases[identity] = base
		return
	}
	if id, ok := node["$id"].(string); ok {
		if r.draft7 && strings.HasPrefix(id, "#") {
			r.anchors[schemaDocument(base)+id] = node
		} else {
			uri := resolveSchemaURI(id, base)
			base = schemaDocument(uri)
			r.documents[base] = node
			if len(uri) > len(base)+1 {
				r.anchors[uri] = node
			}
		}
	}
	r.bases[identity] = base
	// The compiler enumerates these schema maps even after reference indexing.
	// Keep only their source ordering, not annotation/extension data order.
	for _, keyword := range []string{"properties", "patternProperties", "dependentSchemas", "dependencies"} {
		if entries, ok := node[keyword].(map[string]interface{}); ok {
			id := reflect.ValueOf(entries).Pointer()
			if keys, found := r.orders[id]; found {
				if r.compilerOrders == nil {
					r.compilerOrders = make(map[uintptr][]string)
				}
				r.compilerOrders[id] = keys
			}
		}
	}
	for _, keyword := range []string{"const", "enum"} {
		r.retainLiteralOrder(node[keyword])
	}
	if r.work != nil {
		r.work.indexNodes++
	}
	if _, ok := node["$dynamicRef"].(string); ok {
		r.hasDynamicRef = true
	}
	if anchor, ok := node["$anchor"].(string); !r.draft7 && ok {
		r.anchors[base+"#"+anchor] = node
	}
	if anchor, ok := node["$dynamicAnchor"].(string); !r.draft7 && ok {
		key := base + "#" + anchor
		r.anchors[key] = node
		if r.dynamicAnchors == nil {
			r.dynamicAnchors = map[string]interface{}{}
		}
		r.dynamicAnchors[key] = node
	}
	for _, key := range toolSchemaSingleKeywords {
		r.index(node[key], base)
	}
	for _, key := range toolSchemaMapKeywords {
		if children, ok := node[key].(map[string]interface{}); ok {
			for _, name := range r.keys(children) {
				r.index(children[name], base)
			}
		}
	}
	for _, key := range toolSchemaArrayKeywords {
		if children, ok := node[key].([]interface{}); ok {
			for _, child := range children {
				r.index(child, base)
			}
		}
	}
}

func (v *toolSchemaValidator) resolve(ref string, from ...map[string]interface{}) interface{} {
	target, _ := v.resolveWithBase(ref, from...)
	return target
}

// resolveWithBase retains the resource base reached by a JSON pointer even
// when the final object was not indexed as a schema (for example default).
func (v *toolSchemaValidator) resolveWithBase(ref string, from ...map[string]interface{}) (target interface{}, targetBase string) {
	r := v.resourceIndex()
	base := r.bases[reflect.ValueOf(v.root).Pointer()]
	if len(from) > 0 {
		if b, ok := r.bases[reflect.ValueOf(from[0]).Pointer()]; ok {
			base = b
		}
	}
	return v.resolveFromBase(ref, base)
}

func (v *toolSchemaValidator) resolveFromBase(ref, base string) (target interface{}, targetBase string) {
	r := v.resourceIndex()
	key := toolSchemaRefKey{base: base, ref: ref}
	if target, found := v.refs[key]; found {
		return target, v.refBases[key]
	}
	defer func() {
		v.refs[key] = target
		v.refBases[key] = targetBase
	}()
	uri := resolveSchemaURI(ref, base)
	document := schemaDocument(uri)
	root, found := r.documents[document]
	if !found {
		root, found = r.loadExternal(document)
		if !found {
			return nil, ""
		}
	}
	targetBase = document
	fragment := ""
	if len(uri) > len(document) {
		var err error
		fragment, err = url.PathUnescape(uri[len(document)+1:])
		if err != nil || !utf8.ValidString(fragment) {
			return nil, ""
		}
	}
	if fragment == "" {
		target = root
	} else if !strings.HasPrefix(fragment, "/") {
		target = r.anchors[document+"#"+fragment]
		if object, ok := target.(map[string]interface{}); ok {
			if b, found := r.bases[reflect.ValueOf(object).Pointer()]; found {
				targetBase = b
			}
		}
	} else {
		target = root
		for _, raw := range strings.Split(fragment[1:], "/") {
			segment := strings.ReplaceAll(strings.ReplaceAll(raw, "~1", "/"), "~0", "~")
			switch current := target.(type) {
			case map[string]interface{}:
				target = current[segment]
			case []interface{}:
				index, err := strconv.Atoi(segment)
				if err != nil || index < 0 || index >= len(current) || strconv.Itoa(index) != segment {
					target = nil
					return nil, ""
				}
				target = current[index]
			default:
				target = nil
				return nil, ""
			}
			if object, ok := target.(map[string]interface{}); ok {
				if b, found := r.bases[reflect.ValueOf(object).Pointer()]; found {
					targetBase = b
				}
			}
		}
	}
	switch target.(type) {
	case map[string]interface{}, bool:
		return target, targetBase
	default:
		target = nil
		return nil, ""
	}
}

// resolvePureDataReference follows only data-only objects consisting solely
// of a string $ref. That shape has no sibling semantics or nested schema
// context to preserve. Other data objects are returned to existing callers
// unchanged; the general context-aware traversal remains separate work.
func (v *toolSchemaValidator) resolvePureDataReference(ref string, from ...map[string]interface{}) interface{} {
	target, base := v.resolveWithBase(ref, from...)
	seen := make(map[toolSchemaRefKey]bool)
	for depth := 0; depth < 256; depth++ {
		node, ok := target.(map[string]interface{})
		if !ok || node == nil {
			return target
		}
		if _, indexed := v.resources.bases[reflect.ValueOf(node).Pointer()]; indexed || len(node) != 1 {
			return target
		}
		next, ok := node["$ref"].(string)
		if !ok {
			return target
		}
		key := toolSchemaRefKey{base: base, ref: next}
		if seen[key] {
			return nil
		}
		seen[key] = true
		target, base = v.resolveFromBase(next, base)
	}
	return nil
}

func (r *toolSchemaResources) readOrder(root interface{}, raw []byte) {
	r.orders = schemaJSONOrders(root, raw)
}

// Collect object keys in JS Object.keys order: integer indices first, then
// insertion order. A decoded Go map cannot represent the latter on its own.
func schemaJSONOrders(root interface{}, raw []byte) map[uintptr][]string {
	orders := make(map[uintptr][]string)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var walk func(interface{}) error
	walk = func(node interface{}) error {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delimiter, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		switch delimiter {
		case '{':
			object, _ := node.(map[string]interface{})
			var keys []string
			seen := make(map[string]bool)
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return err
				}
				key := token.(string)
				if !seen[key] {
					keys = append(keys, key)
					seen[key] = true
				}
				if err := walk(object[key]); err != nil {
					return err
				}
			}
			if object != nil {
				orders[reflect.ValueOf(object).Pointer()] = schemaJSKeys(keys)
			}
		case '[':
			list, _ := node.([]interface{})
			i := 0
			for decoder.More() {
				var item interface{}
				if i < len(list) {
					item = list[i]
				}
				if err := walk(item); err != nil {
					return err
				}
				i++
			}
		}
		_, err = decoder.Token()
		return err
	}
	// The raw input has already passed json.Unmarshal. Fall back to deterministic
	// map order if this second pass cannot recover the original order.
	if err := walk(root); err != nil {
		return nil
	}
	return orders
}

func (r *toolSchemaResources) keys(object map[string]interface{}) []string {
	id := reflect.ValueOf(object).Pointer()
	if keys, ok := r.orders[id]; ok {
		return keys
	}
	if keys, ok := r.compilerOrders[id]; ok {
		return keys
	}
	return schemaJSKeys(sortedSchemaKeys(object))
}

func schemaJSKeys(keys []string) []string {
	index := func(s string) (uint64, bool) {
		n, err := strconv.ParseUint(s, 10, 32)
		return n, err == nil && n < 4294967295 && strconv.FormatUint(n, 10) == s
	}
	sort.SliceStable(keys, func(i, j int) bool {
		a, ai := index(keys[i])
		b, bi := index(keys[j])
		if ai != bi {
			return ai
		}
		return ai && a < b
	})
	return keys
}
