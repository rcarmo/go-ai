package goai

import (
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
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

type toolSchemaWork struct{ convertCalls, indexNodes, regexChecks, literalCandidates int } // Optional structural-work witness in white-box tests.

type toolSchemaValidator struct {
	root            map[string]interface{}
	schemaRaw       []byte
	resources       *toolSchemaResources
	refs            map[toolSchemaRefKey]interface{}
	refBases        map[toolSchemaRefKey]string
	regex           map[string]toolSchemaRegexResult
	patterns        map[toolSchemaPatternKey]toolSchemaPatternResult
	patternSyntax   map[string]error
	patternDeadline time.Time
	patternFailure  error
	converted       map[toolSchemaMemoKey]interface{}
	converting      map[toolSchemaMemoKey]bool
	checked         map[toolSchemaMemoKey]bool
	checking        map[toolSchemaMemoKey]bool
	members         map[uintptr]*toolSchemaMembers
	// Pointer identities must not outlive their inputs: retain them through
	// the call so GC/address reuse cannot turn a different candidate into a hit.
	inputs map[toolSchemaMemoKey]interface{}
	cuts   uint64
	work   *toolSchemaWork
}

func newToolSchemaValidator(root map[string]interface{}, raw ...[]byte) *toolSchemaValidator {
	v := &toolSchemaValidator{
		root: root, refs: make(map[toolSchemaRefKey]interface{}), refBases: make(map[toolSchemaRefKey]string),
		converted: make(map[toolSchemaMemoKey]interface{}), converting: make(map[toolSchemaMemoKey]bool),
		checked: make(map[toolSchemaMemoKey]bool), checking: make(map[toolSchemaMemoKey]bool),
		members: make(map[uintptr]*toolSchemaMembers), inputs: make(map[toolSchemaMemoKey]interface{}),
	}
	if len(raw) > 0 {
		v.schemaRaw = raw[0]
	}
	return v
}

func (v *toolSchemaValidator) convertReference(value interface{}, ref string, from ...map[string]interface{}) interface{} {
	target, ok := v.resolvePureDataReference(ref, from...).(map[string]interface{})
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

func (v *toolSchemaValidator) checkReference(value interface{}, ref string, from ...map[string]interface{}) bool {
	target := v.resolvePureDataReference(ref, from...)
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
	source string
	schema interface{}
}

type toolSchemaMembers struct {
	validator  *toolSchemaValidator
	properties map[string]interface{}
	patterns   []toolSchemaPattern
	additional interface{}
}

func (v *toolSchemaValidator) membersFor(schema map[string]interface{}) *toolSchemaMembers {
	id := reflect.ValueOf(schema).Pointer()
	if members := v.members[id]; members != nil {
		return members
	}
	m := &toolSchemaMembers{validator: v, additional: schema["additionalProperties"]}
	m.properties, _ = schema["properties"].(map[string]interface{})
	patterns, _ := schema["patternProperties"].(map[string]interface{})
	for _, pattern := range sortedSchemaKeys(patterns) {
		m.patterns = append(m.patterns, toolSchemaPattern{source: pattern, schema: patterns[pattern]})
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
		result := m.validator.matchSchemaPattern(pattern.source, key)
		if result.err != nil || !result.valid {
			return false
		}
		if result.matched {
			matched = true
			if !visit(pattern.schema) {
				return false
			}
		}
	}
	if !declared && !matched && m.additional != nil {
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
	types := schemaTypes(schema["type"])
	if len(types) > 0 && !anyMatchesJSONSchemaType(value, types) {
		return &toolSchemaIssue{path, "expected " + strings.Join(types, " or ") + ", got " + schemaLiteralTypeName(value)}
	}
	if values, ok := schema["enum"].([]interface{}); ok {
		found := false
		for _, member := range values {
			if schemaLiteralEqual(value, member) {
				found = true
				break
			}
		}
		if !found {
			return &toolSchemaIssue{path, "expected one of " + v.literalEnumList(values)}
		}
	}
	if literal, exists := schema["const"]; exists && !schemaLiteralEqual(value, literal) {
		return &toolSchemaIssue{path, "expected " + v.literalJSON(literal)}
	}
	if reason := schemaNumericBoundsReason(value, schema); reason != "" {
		return &toolSchemaIssue{path, reason}
	}
	if issue := v.constraintIssue(value, schema, path, active); issue != nil {
		return issue
	}
	if _, ok := value.(string); ok {
		if reason := v.stringPatternReason(value, schema); reason != "" {
			return &toolSchemaIssue{path, reason}
		}
		if reason := v.stringFormatReason(value, schema); reason != "" {
			return &toolSchemaIssue{path, reason}
		}
	}
	if object, ok := value.(map[string]interface{}); ok {
		if required, ok := schema["required"].([]interface{}); ok {
			for _, raw := range required {
				if key, ok := raw.(string); ok {
					if _, present := object[key]; !present {
						return &toolSchemaIssue{schemaIssuePath(path, key), "missing"}
					}
				}
			}
		}
		members := v.membersFor(schema)
		// Declared property errors precede additional-property errors upstream.
		keys := []string{}
		for _, key := range v.resourceIndex().keys(members.properties) {
			if _, present := object[key]; present {
				keys = append(keys, key)
			}
		}
		for _, key := range sortedSchemaKeys(object) {
			if _, declared := members.properties[key]; !declared {
				keys = append(keys, key)
			}
		}
		for _, key := range keys {
			var failure *toolSchemaIssue
			declared := false
			_, declared = members.properties[key]
			matched := declared
			for _, p := range members.patterns {
				if v.matchSchemaPattern(p.source, key).matched {
					matched = true
				}
			}
			accepted := members.each(key, func(raw interface{}) bool {
				if v.acceptsRaw(object[key], raw) {
					return true
				}
				childPath := schemaIssuePath(path, key)
				if no, ok := raw.(bool); ok && !no && !matched {
					failure = &toolSchemaIssue{childPath, "unknown field"}
				} else {
					failure = v.rawIssue(object[key], raw, childPath, active)
				}
				return false
			})
			if failure != nil {
				return failure
			}
			if !accepted {
				return &toolSchemaIssue{schemaIssuePath(path, key), "pattern property checker unavailable or invalid"}
			}
		}
	}
	if array, ok := value.([]interface{}); ok {
		for i, current := range array {
			if raw := validationItemRaw(schema, i); raw != nil && !v.acceptsRaw(current, raw) {
				return v.rawIssue(current, raw, schemaIssuePath(path, strconv.Itoa(i)), active)
			}
		}
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		if branches, ok := schemaList(schema[keyword]); ok {
			matches := 0
			for _, branch := range branches {
				if v.schemaAcceptsValue(value, branch) {
					matches++
				}
			}
			if keyword == "oneOf" && matches > 1 {
				return &toolSchemaIssue{path, "matches more than one alternative"}
			}
			if keyword != "allOf" && matches > 0 {
				continue
			}
			var best *toolSchemaIssue
			bestDeclared := -1
			for _, branch := range branches {
				if v.schemaAcceptsValue(value, branch) {
					continue
				}
				candidate := v.issue(value, branch, path, active)
				if keyword == "allOf" {
					return candidate
				}
				declared := 0
				resolved := branch
				if ref, ok := branch["$ref"].(string); ok {
					if target, ok := v.resolvePureDataReference(ref, branch).(map[string]interface{}); ok {
						resolved = target
					}
				}
				if object, ok := value.(map[string]interface{}); ok {
					properties, _ := resolved["properties"].(map[string]interface{})
					for key := range object {
						if _, present := properties[key]; present {
							declared++
						}
					}
				}
				if best == nil || declared > bestDeclared || declared == bestDeclared && len(candidate.path) > len(best.path) {
					best, bestDeclared = candidate, declared
				}
			}
			if best != nil {
				return best
			}
		}
	}
	if ref, ok := schema["$ref"].(string); ok && !v.checkReference(value, ref, schema) {
		target := v.resolvePureDataReference(ref, schema)
		if target == nil {
			return &toolSchemaIssue{path, "unresolved reference " + ref}
		}
		if child, ok := target.(map[string]interface{}); ok {
			key, cacheable := toolSchemaKey(child, value)
			if cacheable && !active[key] {
				active[key] = true
				defer delete(active, key)
				return v.issue(value, child, path, active)
			}
		} else {
			return v.rawIssue(value, target, path, active)
		}
	}
	return generic
}

var _ error = (*toolSchemaIssue)(nil)
