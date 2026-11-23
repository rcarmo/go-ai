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
	var parsed any
	if err := json.Unmarshal(schema, &parsed); err != nil {
		return nil, fmt.Errorf("decode schema: %w", err)
	}
	var root map[string]any
	switch s := parsed.(type) {
	case map[string]any:
		root = s
	case bool:
	default:
		return nil, fmt.Errorf("schema must be an object or a boolean")
	}
	valueRaw, hasRawValue := value.(json.RawMessage)
	if hasRawValue {
		var decoded any
		if err := json.Unmarshal(valueRaw, &decoded); err != nil {
			return nil, fmt.Errorf("decode value: %w", err)
		}
		value = decoded
	}
	v, err := newToolSchemaResourceValidator(root, schema, resources, draft)
	if err != nil {
		return nil, err
	}
	accepted := v.acceptsRaw(value, parsed)
	if v.patternFailure != nil {
		return nil, fmt.Errorf("pattern checker unavailable: %w", v.patternFailure)
	}
	if accepted {
		return []JSONSchemaDiagnostic{}, nil
	}
	if hasRawValue {
		v.valueOrders = schemaJSONOrders(value, valueRaw)
	}
	mode := diagnosticsStructured
	if v.trackDynamic {
		mode = diagnosticsDetached
	}
	issues := v.appendIssues(nil, value, parsed, "", make(map[toolSchemaMemoKey]bool), mode)
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
