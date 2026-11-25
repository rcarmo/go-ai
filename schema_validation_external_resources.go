package goai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
)

type toolSchemaExternalResource struct {
	schema any
	raw    json.RawMessage
}

// JSONSchemaReferenceDraft controls how references and identifiers are resolved.
type JSONSchemaReferenceDraft string

const (
	JSONSchemaReferenceDraft202012 JSONSchemaReferenceDraft = "2020-12"
	JSONSchemaReferenceDraft7      JSONSchemaReferenceDraft = "7"
)

// JSONSchemaCheckOptions configures a standalone check. Resources maps
// retrieval URIs to host-supplied JSON schemas; no references are fetched.
// A zero Draft selects 2020-12. AssertFormats enables the built-in format
// checks. Unknown format names always remain annotations.
type JSONSchemaCheckOptions struct {
	Resources     map[string]json.RawMessage
	Draft         JSONSchemaReferenceDraft
	AssertFormats bool
}

// CheckJSONSchemaValueWithOptions checks a value without coercion. Pass a
// decoded value, json.RawMessage, or typed []byte for the bytes type.
// Its zero options treat format as an
// annotation; CheckJSONSchemaValue and tool validation retain their existing
// format checks. A false result with no error means the value failed the
// schema. Errors report setup or checker failures.
func CheckJSONSchemaValueWithOptions(schema json.RawMessage, value any, options JSONSchemaCheckOptions) (bool, error) {
	var drafts []JSONSchemaReferenceDraft
	if options.Draft != "" {
		drafts = []JSONSchemaReferenceDraft{options.Draft}
	}
	call, err := prepareStandaloneSchemaValue(schema, value, options.Resources, drafts)
	if err != nil {
		return false, err
	}
	v := call.validator
	v.formatPolicy = configuredSchemaFormats(options.AssertFormats)
	return v.checkStandaloneValue(call.schema, call.value)
}

func (v *toolSchemaValidator) checkStandaloneValue(schema, value any) (bool, error) {
	v.useStandaloneNumericTypes()
	accepted := v.acceptsRaw(value, schema)
	if v.patternFailure != nil {
		return false, fmt.Errorf("pattern checker unavailable: %w", v.patternFailure)
	}
	return accepted, nil
}

// ValidateJSONSchemaValueWithResources validates a decoded JSON value without
// coercing it. The keys in resources are retrieval URIs; each value is a JSON
// Schema object or boolean. Retrieval URIs must identify distinct documents
// after URI normalization. The validation owns its registry and never fetches
// missing documents. It does not modify the schema, value, or resources.
// The optional reference draft applies to all documents, not just the root.
// It defaults to draft 2020-12 and is not inferred from $schema.
func ValidateJSONSchemaValueWithResources(schema map[string]any, value any, resources map[string]json.RawMessage, draft ...JSONSchemaReferenceDraft) error {
	v, err := newToolSchemaResourceValidator(schema, nil, resources, draft)
	if err != nil {
		return err
	}
	accepted := v.schemaAcceptsValue(value, schema)
	if v.patternFailure != nil {
		return fmt.Errorf("pattern checker unavailable: %w", v.patternFailure)
	}
	if accepted {
		return nil
	}
	issue := v.issue(value, schema, "", make(map[toolSchemaMemoKey]bool))
	if v.patternFailure != nil {
		return fmt.Errorf("pattern checker unavailable: %w", v.patternFailure)
	}
	return issue
}

// ExplainJSONSchemaValueWithResources reports why a JSON value fails
// validation without coercing it. It returns an empty slice for a valid value.
// Pass a decoded JSON value, or pass json.RawMessage to preserve the order of
// its object keys. When visiting fields in instance order, array-index keys
// sort numerically first. Other keys follow their order in the raw JSON, or
// lexical order for a decoded Go map.
// Diagnostics can omit other failures at the same node. A dynamic reference
// can reject a value in scope but yield no local diagnostic. Such failures are
// reported at the root.
//
// Resources and draft work as in ValidateJSONSchemaValueWithResources. A nil
// resource map registers no host documents. Errors report an input or checker
// failure instead of an instance diagnostic.
func ExplainJSONSchemaValueWithResources(schema json.RawMessage, value any, resources map[string]json.RawMessage, draft ...JSONSchemaReferenceDraft) ([]JSONSchemaDiagnostic, error) {
	call, err := prepareStandaloneSchemaValue(schema, value, resources, draft)
	if err != nil {
		return nil, err
	}
	return call.validator.explainSchemaValue(call.schema, call.value, call.valueRaw)
}

// CoerceJSONSchemaValueWithResources applies the JSON Schema coercion rules
// without requiring the result to pass validation. It leaves values that cannot
// be converted as they are and does not modify the input. Schema, value,
// resources, and draft work as in ExplainJSONSchemaValueWithResources.
func CoerceJSONSchemaValueWithResources(schema json.RawMessage, value any, resources map[string]json.RawMessage, draft ...JSONSchemaReferenceDraft) (any, error) {
	call, err := prepareStandaloneSchemaValue(schema, value, resources, draft)
	if err != nil {
		return nil, err
	}
	return call.coerce()
}

// ParseJSONSchemaValueWithResources coerces a value and checks the result. A
// valid value returns an empty diagnostic slice. An invalid value returns nil
// and the structured diagnostics of the coerced value, using deterministic
// key order for decoded maps. Errors report setup or checker failures.
// Schema, value, resources, and draft work as in ExplainJSONSchemaValueWithResources.
func ParseJSONSchemaValueWithResources(schema json.RawMessage, value any, resources map[string]json.RawMessage, draft ...JSONSchemaReferenceDraft) (any, []JSONSchemaDiagnostic, error) {
	call, err := prepareStandaloneSchemaValue(schema, value, resources, draft)
	if err != nil {
		return nil, nil, err
	}
	return call.parse()
}

type standaloneSchemaValue struct {
	validator *toolSchemaValidator
	schema    any
	value     any
	valueRaw  json.RawMessage
}

func (call standaloneSchemaValue) coerce() (any, error) {
	v := call.validator
	value := call.value
	if root, ok := call.schema.(map[string]any); ok {
		value = v.normalizeOptionalNulls(value, root)
		value = v.convertValidationValue(value, root)
		value = v.coerceValidationTypes(value, root)
	}
	if v.patternFailure != nil {
		return nil, fmt.Errorf("pattern checker unavailable: %w", v.patternFailure)
	}
	return value, nil
}

func (call standaloneSchemaValue) validate() (any, []JSONSchemaDiagnostic, error) {
	diagnostics, err := call.validator.explainSchemaValue(call.schema, call.value, call.valueRaw)
	if err != nil || len(diagnostics) != 0 {
		return nil, diagnostics, err
	}
	return call.value, diagnostics, nil
}

func (call standaloneSchemaValue) parse() (any, []JSONSchemaDiagnostic, error) {
	coerced, err := call.coerce()
	if err != nil {
		return nil, nil, err
	}
	call.value, call.valueRaw = coerced, nil
	return call.validate()
}

func prepareStandaloneSchemaValue(schema json.RawMessage, value any, resources map[string]json.RawMessage, draft []JSONSchemaReferenceDraft) (standaloneSchemaValue, error) {
	var parsed any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		return standaloneSchemaValue{}, fmt.Errorf("decode schema: %w", err)
	}
	var root map[string]any
	switch s := parsed.(type) {
	case map[string]any:
		root = s
	case bool:
	default:
		return standaloneSchemaValue{}, fmt.Errorf("schema must be an object or a boolean")
	}
	value, valueRaw, err := standaloneSchemaInput(value)
	if err != nil {
		return standaloneSchemaValue{}, err
	}
	v, err := newToolSchemaResourceValidator(root, schema, resources, draft)
	if err != nil {
		return standaloneSchemaValue{}, err
	}
	return standaloneSchemaValue{validator: v, schema: parsed, value: value, valueRaw: valueRaw}, nil
}

func standaloneSchemaInput(value any) (any, json.RawMessage, error) {
	if raw, ok := value.(json.RawMessage); ok {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, nil, fmt.Errorf("decode value: %w", err)
		}
		return decoded, raw, nil
	}
	return value, nil, nil
}

func (v *toolSchemaValidator) explainSchemaValue(schema, value any, valueRaw json.RawMessage) ([]JSONSchemaDiagnostic, error) {
	v.useStandaloneNumericTypes()
	accepted := v.acceptsRaw(value, schema)
	if v.patternFailure != nil {
		return nil, fmt.Errorf("pattern checker unavailable: %w", v.patternFailure)
	}
	if accepted {
		return []JSONSchemaDiagnostic{}, nil
	}
	if len(valueRaw) != 0 {
		v.valueOrders = schemaJSONOrders(value, valueRaw)
	}
	mode := diagnosticsStructured
	if v.trackDynamic {
		mode = diagnosticsDetached
	}
	issues := v.appendIssues(nil, value, schema, "", make(map[toolSchemaMemoKey]bool), mode)
	if len(issues) == 0 {
		// A detached walk can miss a failure that depends on a dynamic scope.
		issues = append(issues, JSONSchemaDiagnostic{Keyword: "schema", Message: "invalid"})
	}
	if v.patternFailure != nil {
		return nil, fmt.Errorf("pattern checker unavailable: %w", v.patternFailure)
	}
	return issues, nil
}

func newToolSchemaResourceValidator(schema map[string]any, raw json.RawMessage, resources map[string]json.RawMessage, draft []JSONSchemaReferenceDraft) (*toolSchemaValidator, error) {
	if len(draft) > 1 {
		return nil, fmt.Errorf("expected at most one schema reference draft")
	}
	if len(draft) == 1 && draft[0] != JSONSchemaReferenceDraft202012 && draft[0] != JSONSchemaReferenceDraft7 {
		return nil, fmt.Errorf("unsupported schema reference draft %q", draft[0])
	}
	v := newToolSchemaValidator(schema, raw)
	v.draft7 = len(draft) == 1 && draft[0] == JSONSchemaReferenceDraft7
	r := v.resourceIndex()
	if err := r.registerExternal(resources); err != nil {
		return nil, err
	}
	// A lazily loaded document can introduce a dynamic reference not present in
	// the root. Track the resource stack from the start when using a registry.
	v.trackDynamic = !v.draft7 && (r.hasDynamicRef || len(r.external) > 0)
	return v, nil
}

func (r *toolSchemaResources) registerExternal(resources map[string]json.RawMessage) error {
	if len(resources) == 0 {
		return nil
	}
	r.external = make(map[string]toolSchemaExternalResource, len(resources))
	for _, uri := range slices.Sorted(maps.Keys(resources)) {
		raw := bytes.Clone(resources[uri])
		var schema any
		if err := json.Unmarshal(raw, &schema); err != nil {
			return fmt.Errorf("schema resource %q: %w", uri, err)
		}
		switch schema.(type) {
		case map[string]any, bool:
		default:
			return fmt.Errorf("schema resource %q: expected an object or boolean", uri)
		}
		document := schemaDocument(resolveSchemaURI(uri, toolSchemaDefaultBase))
		if _, exists := r.external[document]; exists {
			return fmt.Errorf("duplicate schema resource for %q", document)
		}
		r.external[document] = toolSchemaExternalResource{schema: schema, raw: raw}
	}
	return nil
}

func (r *toolSchemaResources) loadExternal(document string) (any, bool) {
	resource, found := r.external[document]
	if !found {
		return nil, false
	}
	r.documents[document] = resource.schema // Retain retrieval URI even if $id supplies another base.
	r.readOrder(resource.schema, resource.raw)
	r.index(resource.schema, document)
	r.orders = nil
	delete(r.external, document)
	return resource.schema, true
}
