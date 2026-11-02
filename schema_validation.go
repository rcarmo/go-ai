package goai

import (
	"math"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// All reference state is owned by one validation call. Schema/value identity,
// not value equality, bounds repeated work without sharing mutable results
// across calls. The parsed schema stays live for the lifetime of these keys.
type toolSchemaMemoKey struct {
	schema uintptr
	value  toolSchemaValueKey
}

type toolSchemaValueKey struct {
	typ    reflect.Type
	ptr    uintptr
	length int
	bits   uint64
	scalar interface{}
}

func toolSchemaKey(schema map[string]interface{}, value interface{}) (toolSchemaMemoKey, bool) {
	key := toolSchemaMemoKey{schema: reflect.ValueOf(schema).Pointer()}
	if value == nil {
		return key, true
	}
	r := reflect.ValueOf(value)
	key.value.typ = r.Type()
	switch r.Kind() {
	case reflect.Map:
		key.value.ptr = r.Pointer()
	case reflect.Slice:
		key.value.ptr, key.value.length = r.Pointer(), r.Len()
	case reflect.Float64, reflect.Float32:
		// NaN is not a reflexive map key; bits also distinguish signed zero.
		key.value.bits = math.Float64bits(r.Float())
	case reflect.Bool, reflect.String, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		key.value.scalar = value
	default:
		return key, false
	}
	return key, true
}

type toolSchemaWork struct{ convertCalls int } // Optional structural-work witness in white-box tests.

type toolSchemaValidator struct {
	root       map[string]interface{}
	refs       map[string]interface{}
	converted  map[toolSchemaMemoKey]interface{}
	converting map[toolSchemaMemoKey]bool
	checked    map[toolSchemaMemoKey]bool
	checking   map[toolSchemaMemoKey]bool
	members    map[uintptr]*toolSchemaMembers
	// Pointer identities must not outlive their inputs: retain them through
	// the call so GC/address reuse cannot turn a different candidate into a hit.
	inputs map[toolSchemaMemoKey]interface{}
	cuts   uint64
	work   *toolSchemaWork
}

func newToolSchemaValidator(root map[string]interface{}) *toolSchemaValidator {
	return &toolSchemaValidator{
		root: root, refs: make(map[string]interface{}),
		converted: make(map[toolSchemaMemoKey]interface{}), converting: make(map[toolSchemaMemoKey]bool),
		checked: make(map[toolSchemaMemoKey]bool), checking: make(map[toolSchemaMemoKey]bool),
		members: make(map[uintptr]*toolSchemaMembers), inputs: make(map[toolSchemaMemoKey]interface{}),
	}
}

// Resolve document-local URI-fragment JSON pointers only. Unknown resources
// reject locally; validation never fetches a URL, file, credential or metadata.
func (v *toolSchemaValidator) resolve(ref string) interface{} {
	if target, found := v.refs[ref]; found {
		return target
	}
	var target interface{}
	defer func() { v.refs[ref] = target }()
	if !strings.HasPrefix(ref, "#") {
		return nil
	}
	pointer, err := url.PathUnescape(ref[1:])
	if err != nil || (pointer != "" && !strings.HasPrefix(pointer, "/")) {
		return nil
	}
	target = v.root
	if pointer != "" {
		for _, segment := range strings.Split(pointer[1:], "/") {
			segment = strings.ReplaceAll(strings.ReplaceAll(segment, "~1", "/"), "~0", "~")
			switch current := target.(type) {
			case map[string]interface{}:
				target = current[segment]
			case []interface{}:
				index, parseErr := strconv.Atoi(segment)
				if parseErr != nil || index < 0 || index >= len(current) || strconv.Itoa(index) != segment {
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

func (v *toolSchemaValidator) convertReference(value interface{}, ref string) interface{} {
	target, ok := v.resolve(ref).(map[string]interface{})
	if !ok {
		return value
	}
	key, cacheable := toolSchemaKey(target, value)
	if !cacheable {
		return value
	}
	v.inputs[key] = value
	if converted, found := v.converted[key]; found {
		return converted
	}
	if v.converting[key] {
		v.cuts++
		return value
	}
	v.converting[key] = true
	cuts := v.cuts
	converted := v.convertValidationValue(value, target)
	delete(v.converting, key)
	// A re-entry cutoff depends on the active ancestors. Such a result cannot
	// be reused as an independent conversion of the same node/value pair.
	if cuts == v.cuts {
		v.converted[key] = converted
	}
	return converted
}

func (v *toolSchemaValidator) checkReference(value interface{}, ref string) bool {
	target := v.resolve(ref)
	if literal, ok := target.(bool); ok {
		return literal
	}
	schema, ok := target.(map[string]interface{})
	if !ok {
		return false
	}
	key, cacheable := toolSchemaKey(schema, value)
	if !cacheable {
		return false
	}
	v.inputs[key] = value
	if accepted, found := v.checked[key]; found {
		return accepted
	}
	if v.checking[key] {
		return false
	}
	v.checking[key] = true
	accepted := v.schemaAcceptsValue(value, schema)
	delete(v.checking, key)
	v.checked[key] = accepted
	return accepted
}

func (v *toolSchemaValidator) acceptsRaw(value, raw interface{}) bool {
	switch schema := raw.(type) {
	case bool:
		return schema
	case map[string]interface{}:
		return v.schemaAcceptsValue(value, schema)
	default:
		return true // Existing handling of unsupported schema forms.
	}
}

type toolSchemaPattern struct {
	re     *regexp.Regexp
	schema interface{}
}

type toolSchemaMembers struct {
	properties         map[string]interface{}
	patterns           []toolSchemaPattern
	additional         interface{}
	unsupportedPattern bool
}

func (v *toolSchemaValidator) membersFor(schema map[string]interface{}) *toolSchemaMembers {
	id := reflect.ValueOf(schema).Pointer()
	if members := v.members[id]; members != nil {
		return members
	}
	m := &toolSchemaMembers{additional: schema["additionalProperties"]}
	m.properties, _ = schema["properties"].(map[string]interface{})
	patterns, _ := schema["patternProperties"].(map[string]interface{})
	for _, pattern := range sortedSchemaKeys(patterns) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			m.unsupportedPattern = true
			continue
		}
		m.patterns = append(m.patterns, toolSchemaPattern{re: re, schema: patterns[pattern]})
	}
	v.members[id] = m
	return m
}

func (m *toolSchemaMembers) hasSchemas() bool {
	return len(m.properties) > 0 || len(m.patterns) > 0 || m.additional != nil
}

func (m *toolSchemaMembers) each(key string, visit func(interface{}) bool) bool {
	property, declared := m.properties[key]
	if declared && !visit(property) {
		return false
	}
	matched := false
	for _, pattern := range m.patterns {
		if pattern.re.MatchString(key) {
			matched = true
			if !visit(pattern.schema) {
				return false
			}
		}
	}
	// Keep the existing conservative additional-property behaviour outside
	// Go's regex domain. Full ECMAScript pattern semantics remain unported.
	if !declared && !matched && !m.unsupportedPattern && m.additional != nil {
		return visit(m.additional)
	}
	return true
}

func sortedSchemaKeys(object map[string]interface{}) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type toolSchemaIssue struct{ path, reason string }

func (i *toolSchemaIssue) Error() string {
	path := i.path
	if path == "" {
		path = "root"
	}
	return path + ": " + i.reason
}

// Match explain.ts listValues: truncate before serializing, not after building
// a potentially enormous message. SafeJSONStringify preserves JS string escapes.
func schemaEnumList(values []interface{}) string {
	const listed = 20
	if len(values) <= listed {
		return SafeJSONStringify(values)
	}
	return strings.TrimSuffix(SafeJSONStringify(values[:listed]), "]") + ", ... and " + strconv.Itoa(len(values)-listed) + " more]"
}

func schemaIssuePath(parent, key string) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}

// Select the first failing branch/required member without repeating conversion.
// Reference-check memo entries hold booleans, never a path from another caller.
func (v *toolSchemaValidator) issue(value interface{}, schema map[string]interface{}, path string, active map[toolSchemaMemoKey]bool) *toolSchemaIssue {
	generic := &toolSchemaIssue{path, "value does not match schema"}
	if ref, ok := schema["$ref"].(string); ok && !v.checkReference(value, ref) {
		if target, ok := v.resolve(ref).(map[string]interface{}); ok {
			key, cacheable := toolSchemaKey(target, value)
			if cacheable && !active[key] {
				active[key] = true
				defer delete(active, key)
				return v.issue(value, target, path, active)
			}
		}
		return generic
	}
	types := schemaTypes(schema["type"])
	if len(types) == 0 || anyMatchesJSONSchemaType(value, types) {
		if reason := schemaNumericBoundsReason(value, schema); reason != "" {
			return &toolSchemaIssue{path, reason}
		}
	}
	if _, isString := value.(string); isString && (len(types) == 0 || anyMatchesJSONSchemaType(value, types)) {
		if !schemaConstraintsMatch(value, "string", schema) {
			return &toolSchemaIssue{path, "expected one of " + schemaEnumList(schema["enum"].([]interface{}))}
		}
		if reason := schemaStringFormatReason(value, schema); reason != "" {
			return &toolSchemaIssue{path, reason}
		}
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		if branches, ok := schemaList(schema[keyword]); ok {
			for _, branch := range branches {
				if !v.schemaAcceptsValue(value, branch) {
					return v.issue(value, branch, path, active)
				}
			}
		}
	}
	if object, ok := value.(map[string]interface{}); ok {
		if required, ok := schema["required"].([]interface{}); ok {
			for _, raw := range required {
				if key, ok := raw.(string); ok {
					if _, exists := object[key]; !exists {
						return &toolSchemaIssue{schemaIssuePath(path, key), "missing"}
					}
				}
			}
		}
		members := v.membersFor(schema)
		for _, key := range sortedSchemaKeys(object) {
			var failure *toolSchemaIssue
			members.each(key, func(raw interface{}) bool {
				if v.acceptsRaw(object[key], raw) {
					return true
				}
				childPath := schemaIssuePath(path, key)
				if child, ok := raw.(map[string]interface{}); ok {
					failure = v.issue(object[key], child, childPath, active)
				} else {
					failure = &toolSchemaIssue{childPath, "value does not match schema"}
				}
				return false
			})
			if failure != nil {
				return failure
			}
		}
	}
	if array, ok := value.([]interface{}); ok {
		for index, current := range array {
			if child := validationItemSchema(schema, index); child != nil && !v.schemaAcceptsValue(current, child) {
				return v.issue(current, child, schemaIssuePath(path, strconv.Itoa(index)), active)
			}
		}
	}
	return generic
}

var _ error = (*toolSchemaIssue)(nil)
