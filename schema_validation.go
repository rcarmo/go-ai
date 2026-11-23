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
	scope  string
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

// Optional structural-work witness in white-box tests.
type toolSchemaWork struct{ convertCalls, indexNodes, regexChecks, literalCandidates, diagnosticRefs int }

type toolSchemaDiagnosticKey struct {
	ref  toolSchemaMemoKey
	path string
	mode toolSchemaDiagnosticMode
}

type toolSchemaValidator struct {
	root            map[string]interface{}
	draft7          bool
	trackDynamic    bool
	scope           []string
	scopeKey        string
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
	diagnostics     map[toolSchemaDiagnosticKey]toolSchemaIssues
	diagnosticCuts  uint64
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
	target, base := v.referenceTarget(ref, false, from...)
	return v.checkReferenceTarget(value, target, base)
}

func (v *toolSchemaValidator) checkReferenceTarget(value, target interface{}, base string) bool {
	if literal, ok := target.(bool); ok {
		return literal
	}
	schema, ok := target.(map[string]interface{})
	if !ok {
		return false
	}
	if base != "" {
		defer v.enterSchemaScope(base)()
	}
	key, cacheable := toolSchemaKey(schema, value)
	key.scope = v.scopeKey
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

func validToolSchemaBranch(raw interface{}) bool {
	switch raw.(type) {
	case bool, map[string]interface{}:
		return true
	default:
		return false
	}
}

func (v *toolSchemaValidator) schemaApplicatorMatches(value interface{}, branches []interface{}) (matches, count int) {
	for _, branch := range branches {
		if !validToolSchemaBranch(branch) {
			continue
		}
		count++
		if v.acceptsRaw(value, branch) {
			matches++
		}
	}
	return matches, count
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

// JSONSchemaDiagnostic explains one failure. Path is a JSON Pointer into the
// instance (empty for the root), and Keyword identifies the failed check.
type JSONSchemaDiagnostic struct {
	Path    string `json:"path"`
	Keyword string `json:"keyword"`
	Message string `json:"message"`
}

type toolSchemaIssue JSONSchemaDiagnostic

type toolSchemaDiagnosticMode uint8

const (
	diagnosticFirst     toolSchemaDiagnosticMode = iota // First error in the validation scope.
	diagnosticsScoped                                   // Model-facing errors in that scope.
	diagnosticsDetached                                 // Pinned standalone explanation of dynamic references.
)

var schemaPointerEscape = strings.NewReplacer("~", "~0", "/", "~1")
var schemaPointerUnescape = strings.NewReplacer("~1", "/", "~0", "~")

func schemaIssuePath(parent, key string) string {
	return parent + "/" + schemaPointerEscape.Replace(key)
}

func (i *toolSchemaIssue) Error() string {
	path := "root"
	if i.Path != "" {
		// Convert pointer delimiters before unescaping slashes in keys.
		path = schemaPointerUnescape.Replace(strings.ReplaceAll(i.Path[1:], "/", "."))
	}
	return path + ": " + i.Message
}

// Select a first diagnostic for callers whose API returns one error. The tool
// failure protocol instead renders each diagnostic found by the same walk.
func (v *toolSchemaValidator) issue(value interface{}, schema map[string]interface{}, path string, active map[toolSchemaMemoKey]bool) *toolSchemaIssue {
	issues := v.appendIssues(nil, value, schema, path, active, diagnosticFirst)
	return (*toolSchemaIssue)(&issues[0])
}

type toolSchemaIssues []JSONSchemaDiagnostic

func (issues toolSchemaIssues) Error() string {
	var out strings.Builder
	for i := range issues {
		if i > 0 {
			out.WriteByte('\n')
		}
		out.WriteString("  - ")
		out.WriteString((*toolSchemaIssue)(&issues[i]).Error())
	}
	return out.String()
}

// appendIssues explains an already rejected value without repeating conversion.
// Pure membership memo entries hold booleans, never a caller-specific path.
func (v *toolSchemaValidator) appendIssues(out toolSchemaIssues, value, raw interface{}, path string, active map[toolSchemaMemoKey]bool, mode toolSchemaDiagnosticMode) toolSchemaIssues {
	if raw == false {
		return append(out, JSONSchemaDiagnostic{path, "false", "no value is allowed here"})
	}
	schema, ok := raw.(map[string]interface{})
	if !ok {
		return out
	}
	if mode == diagnosticsDetached {
		// The pinned explainer checks each node without the dynamic stack
		// that led to it. Membership (checked before this walk) still uses
		// the full stack.
		if v.acceptsRaw(value, raw) {
			return out
		}
	} else if v.trackDynamic {
		defer v.enterSchemaResource(schema)()
	}
	start := len(out)
	if ref, ok := schema["$ref"].(string); v.draft7 && ok {
		out = v.appendReferenceIssues(out, value, schema, ref, false, path, active, mode)
		if len(out) == start {
			out = append(out, JSONSchemaDiagnostic{path, "schema", "value does not match schema"})
		}
		return out
	}
	types := schemaTypes(schema["type"])
	if len(types) > 0 && !anyMatchesJSONSchemaType(value, types) {
		return append(out, JSONSchemaDiagnostic{path, "type", "expected " + strings.Join(types, " or ") + ", got " + schemaLiteralTypeName(value)})
	}
	if values, ok := schema["enum"].([]interface{}); ok && !schemaContainsLiteral(values, value) {
		out = append(out, JSONSchemaDiagnostic{path, "enum", "expected one of " + v.literalEnumList(values)})
		if mode == diagnosticFirst {
			return out
		}
	}
	if literal, exists := schema["const"]; exists && !SchemaEqual(value, literal) {
		out = append(out, JSONSchemaDiagnostic{path, "const", "expected " + v.literalJSON(literal)})
		if mode == diagnosticFirst {
			return out
		}
	}
	if keyword, reason := schemaNumericBoundFailure(value, schema); reason != "" {
		out = append(out, JSONSchemaDiagnostic{path, keyword, reason})
		if mode == diagnosticFirst {
			return out
		}
	}
	if issue := v.constraintIssue(value, schema, path, active, mode != diagnosticsDetached); issue != nil {
		out = append(out, JSONSchemaDiagnostic(*issue))
		if mode == diagnosticFirst {
			return out
		}
	}
	if _, ok := schemaStringValue(value); ok {
		if reason := v.stringPatternReason(value, schema); reason != "" {
			out = append(out, JSONSchemaDiagnostic{path, "pattern", reason})
			if mode == diagnosticFirst {
				return out
			}
		}
		if reason := v.stringFormatReason(value, schema); reason != "" {
			out = append(out, JSONSchemaDiagnostic{path, "format", reason})
			if mode == diagnosticFirst {
				return out
			}
		}
	}
	switch value := value.(type) {
	case map[string]interface{}:
		out = v.appendObjectIssues(out, value, schema, path, active, mode)
	case []interface{}:
		for i, current := range value {
			if item := validationItemRaw(schema, i); item != nil && !v.acceptsRaw(current, item) {
				out = v.appendIssues(out, current, item, schemaIssuePath(path, strconv.Itoa(i)), active, mode)
				if mode == diagnosticFirst && len(out) > start {
					return out
				}
			}
		}
	}
	if mode == diagnosticFirst && len(out) > start {
		return out
	}
	for _, keyword := range []string{"allOf", "anyOf", "oneOf"} {
		if branches, ok := schema[keyword].([]interface{}); ok {
			out = v.appendUnionIssues(out, value, branches, keyword, path, active, mode)
			if mode == diagnosticFirst && len(out) > start {
				return out
			}
		}
	}
	if mode == diagnosticsDetached {
		if negated, present := schema["not"]; present && v.acceptsRaw(value, negated) {
			out = append(out, JSONSchemaDiagnostic{path, "not", "must not match the schema in not"})
		}
		if selected, present := v.schemaConditionalBranch(value, schema); present {
			out = v.appendIssues(out, value, selected, path, active, mode)
		}
	}
	for _, keyword := range []string{"$ref", "$dynamicRef"} {
		dynamic := keyword == "$dynamicRef"
		ref, ok := schema[keyword].(string)
		if !ok || (dynamic && (!v.trackDynamic || mode == diagnosticsDetached)) {
			continue
		}
		out = v.appendReferenceIssues(out, value, schema, ref, dynamic, path, active, mode)
		if mode == diagnosticFirst && len(out) > start {
			return out
		}
	}
	if len(out) > start {
		return out
	}
	switch value := value.(type) {
	case map[string]interface{}:
		if key, fails := v.firstUnevaluatedPropertyFailure(value, schema, mode != diagnosticsDetached); fails {
			childPath := schemaIssuePath(path, key)
			raw := schema["unevaluatedProperties"]
			if raw == false {
				return append(out, JSONSchemaDiagnostic{childPath, "unevaluatedProperties", "unknown field"})
			}
			return v.appendIssues(out, value[key], raw, childPath, active, mode)
		}
	case []interface{}:
		if index := v.firstUnevaluatedItemFailure(value, schema, mode != diagnosticsDetached); index >= 0 {
			return v.appendIssues(out, value[index], schema["unevaluatedItems"], schemaIssuePath(path, strconv.Itoa(index)), active, mode)
		}
	}
	if mode == diagnosticsDetached {
		if _, dynamic := schema["$dynamicRef"].(string); dynamic {
			return append(out, JSONSchemaDiagnostic{path, "$dynamicRef", "does not match the dynamically referenced schema"})
		}
		return out
	}
	return append(out, JSONSchemaDiagnostic{path, "schema", "value does not match schema"})
}

func (v *toolSchemaValidator) appendObjectIssues(out toolSchemaIssues, object, schema map[string]interface{}, path string, active map[toolSchemaMemoKey]bool, mode toolSchemaDiagnosticMode) toolSchemaIssues {
	initial := len(out)
	if required, ok := schema["required"].([]interface{}); ok {
		for _, raw := range required {
			if key, ok := raw.(string); ok {
				if _, present := object[key]; !present {
					out = append(out, JSONSchemaDiagnostic{schemaIssuePath(path, key), "required", "missing"})
					if mode == diagnosticFirst {
						return out
					}
				}
			}
		}
	}
	members := v.membersFor(schema)
	for _, key := range v.resourceIndex().keys(members.properties) {
		if current, present := object[key]; present && !v.acceptsRaw(current, members.properties[key]) {
			out = v.appendIssues(out, current, members.properties[key], schemaIssuePath(path, key), active, mode)
			if mode == diagnosticFirst && len(out) > initial {
				return out
			}
		}
	}
	// A decoded Go map has no insertion order. Match JS's numeric-key order
	// then use a stable lexical order for its remaining keys.
	for _, key := range schemaJSKeys(sortedSchemaKeys(object)) {
		current := object[key]
		childPath := schemaIssuePath(path, key)
		_, matched := members.properties[key]
		for _, p := range members.patterns {
			result := v.matchSchemaPattern(p.source, key)
			if result.err != nil || !result.valid {
				return append(out, JSONSchemaDiagnostic{childPath, "patternProperties", "pattern property checker unavailable or invalid"})
			}
			if result.matched {
				matched = true
				if !v.acceptsRaw(current, p.schema) {
					out = v.appendIssues(out, current, p.schema, childPath, active, mode)
					if mode == diagnosticFirst && len(out) > initial {
						return out
					}
				}
			}
		}
		if !matched && members.additional != nil && !v.acceptsRaw(current, members.additional) {
			if members.additional == false {
				out = append(out, JSONSchemaDiagnostic{childPath, "additionalProperties", "unknown field"})
			} else {
				out = v.appendIssues(out, current, members.additional, childPath, active, mode)
			}
			if mode == diagnosticFirst && len(out) > initial {
				return out
			}
		}
	}
	return out
}

func (v *toolSchemaValidator) appendUnionIssues(out toolSchemaIssues, value interface{}, branches []interface{}, keyword, path string, active map[toolSchemaMemoKey]bool, mode toolSchemaDiagnosticMode) toolSchemaIssues {
	matches, count := v.schemaApplicatorMatches(value, branches)
	if count == 0 || keyword == "allOf" && matches == count || keyword == "oneOf" && matches == 1 || keyword == "anyOf" && matches > 0 {
		return out
	}
	if keyword == "oneOf" && matches > 1 {
		return append(out, JSONSchemaDiagnostic{path, keyword, "matches more than one alternative"})
	}
	object, isObject := value.(map[string]interface{})
	if keyword != "allOf" && isObject {
		if key, tags := v.unionTag(branches); tags != nil {
			if tag, present := object[key]; present {
				for i, candidate := range tags {
					if SchemaEqual(tag, candidate) {
						return v.appendIssues(out, value, branches[i], path, active, mode)
					}
				}
			}
			return append(out, JSONSchemaDiagnostic{schemaIssuePath(path, key), keyword, "expected one of " + v.literalEnumList(tags)})
		}
	}
	var best toolSchemaIssues
	bestDeclared := -1
	initial := len(out)
	for _, branch := range branches {
		if !validToolSchemaBranch(branch) || v.acceptsRaw(value, branch) {
			continue
		}
		if keyword == "allOf" {
			out = v.appendIssues(out, value, branch, path, active, mode)
			if mode == diagnosticFirst && len(out) > initial {
				return out
			}
			continue
		}
		candidate := v.appendIssues(nil, value, branch, path, active, mode)
		if len(candidate) == 0 {
			continue
		}
		declared := 0
		if isObject {
			properties := v.unionBranchProperties(branch)
			for key := range object {
				if _, present := properties[key]; present {
					declared++
				}
			}
		}
		if len(best) == 0 || declared > bestDeclared || declared == bestDeclared && len(candidate[0].Path) > len(best[0].Path) {
			best, bestDeclared = candidate, declared
		}
	}
	return append(out, best...)
}

func (v *toolSchemaValidator) unionBranchProperties(raw interface{}) map[string]interface{} {
	branch, _ := raw.(map[string]interface{})
	if ref, ok := branch["$ref"].(string); ok {
		branch, _ = v.resolve(ref, branch).(map[string]interface{})
	}
	properties, _ := branch["properties"].(map[string]interface{})
	return properties
}

func (v *toolSchemaValidator) unionTag(branches []interface{}) (string, []interface{}) {
	properties := make([]map[string]interface{}, len(branches))
	for i, branch := range branches {
		properties[i] = v.unionBranchProperties(branch)
	}
	for _, key := range v.resourceIndex().keys(properties[0]) {
		var tags []interface{}
		for _, prop := range properties {
			schema, _ := prop[key].(map[string]interface{})
			tag, hasConst := schema["const"]
			if !hasConst || schemaContainsLiteral(tags, tag) {
				break
			}
			switch tag.(type) {
			case nil, bool, float64, string:
				tags = append(tags, tag)
			}
		}
		if len(tags) == len(properties) {
			return key, tags
		}
	}
	return "", nil
}

func schemaContainsLiteral(values []interface{}, candidate interface{}) bool {
	for _, value := range values {
		if SchemaEqual(value, candidate) {
			return true
		}
	}
	return false
}

func (v *toolSchemaValidator) appendReferenceIssues(out toolSchemaIssues, value interface{}, schema map[string]interface{}, ref string, dynamic bool, path string, active map[toolSchemaMemoKey]bool, mode toolSchemaDiagnosticMode) toolSchemaIssues {
	target, base := v.referenceTarget(ref, dynamic, schema)
	if mode != diagnosticsDetached && v.checkReferenceTarget(value, target, base) {
		return out
	}
	if mode != diagnosticsDetached && base != "" {
		defer v.enterSchemaScope(base)()
	}
	if target == nil {
		return append(out, JSONSchemaDiagnostic{path, "$ref", "unresolved reference " + ref})
	}
	child, ok := target.(map[string]interface{})
	if !ok {
		return v.appendIssues(out, value, target, path, active, mode)
	}
	key, cacheable := toolSchemaKey(child, value)
	key.scope = v.scopeKey
	if !cacheable || active[key] {
		v.diagnosticCuts++
		return out
	}
	// Competing union arms often explain the same reference and value at the
	// same path. A first-only or detached answer, or an answer from a
	// different path, cannot be reused in another explanation.
	memo := toolSchemaDiagnosticKey{ref: key, path: path, mode: mode}
	if issues, found := v.diagnostics[memo]; found {
		return append(out, issues...)
	}
	active[key] = true
	defer delete(active, key)
	if v.work != nil {
		v.work.diagnosticRefs++
	}
	cuts := v.diagnosticCuts
	issues := v.appendIssues(nil, value, child, path, active, mode)
	if cuts == v.diagnosticCuts {
		if v.diagnostics == nil {
			v.diagnostics = make(map[toolSchemaDiagnosticKey]toolSchemaIssues)
		}
		v.diagnostics[memo] = issues
	}
	return append(out, issues...)
}

var _ error = (*toolSchemaIssue)(nil)
var _ error = toolSchemaIssues(nil)
