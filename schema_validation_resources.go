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
	bases         map[uintptr]string
	documents     map[string]interface{}
	anchors       map[string]interface{}
	orders        map[uintptr][]string
	literalOrders map[uintptr][]string
	work          *toolSchemaWork
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
	r := &toolSchemaResources{bases: make(map[uintptr]string), documents: map[string]interface{}{toolSchemaDefaultBase: v.root}, anchors: make(map[string]interface{}), orders: make(map[uintptr][]string)}
	v.resources = r
	r.work = v.work
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
	if id, ok := node["$id"].(string); ok {
		uri := resolveSchemaURI(id, base)
		base = schemaDocument(uri)
		r.documents[base] = node
		if len(uri) > len(base)+1 {
			r.anchors[uri] = node
		}
	}
	r.bases[identity] = base
	for _, keyword := range []string{"const", "enum"} {
		r.retainLiteralOrder(node[keyword])
	}
	if r.work != nil {
		r.work.indexNodes++
	}
	for _, key := range []string{"$anchor", "$dynamicAnchor"} {
		if anchor, ok := node[key].(string); ok {
			r.anchors[base+"#"+anchor] = node
		}
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
	r := v.resourceIndex()
	base := r.bases[reflect.ValueOf(v.root).Pointer()]
	if len(from) > 0 {
		if b, ok := r.bases[reflect.ValueOf(from[0]).Pointer()]; ok {
			base = b
		}
	}
	key := toolSchemaRefKey{base: base, ref: ref}
	if target, found := v.refs[key]; found {
		return target
	}
	var target interface{}
	defer func() { v.refs[key] = target }()
	uri := resolveSchemaURI(ref, base)
	document := schemaDocument(uri)
	root, found := r.documents[document]
	if !found {
		return nil
	}
	fragment := ""
	if len(uri) > len(document) {
		var err error
		fragment, err = url.PathUnescape(uri[len(document)+1:])
		if err != nil || !utf8.ValidString(fragment) {
			return nil
		}
	}
	if fragment == "" {
		target = root
	} else if !strings.HasPrefix(fragment, "/") {
		target = r.anchors[document+"#"+fragment]
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
					return nil
				}
				target = current[index]
			default:
				target = nil
				return nil
			}
		}
	}
	switch target.(type) {
	case map[string]interface{}, bool:
		return target
	default:
		target = nil
		return nil
	}
}

// JSON bytes preserve object-member order that Go maps cannot represent.
// Capture it lazily only when references need indexing; duplicate registrations
// then follow JS Object.keys ordering (integer indices first, then insertion).
func (r *toolSchemaResources) readOrder(root interface{}, raw []byte) {
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
				r.orders[reflect.ValueOf(object).Pointer()] = schemaJSKeys(keys)
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
	// Parameters already passed json.Unmarshal; no malformed input is accepted
	// by this ordering pass. Private map-only callers use deterministic keys.
	if err := walk(root); err != nil {
		r.orders = make(map[uintptr][]string)
	}
}

func (r *toolSchemaResources) keys(object map[string]interface{}) []string {
	if keys, ok := r.orders[reflect.ValueOf(object).Pointer()]; ok {
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
