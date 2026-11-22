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

// ValidateJSONSchemaValueWithResources validates a decoded JSON value without
// coercing it. The keys in resources are retrieval URIs; each value is a JSON
// Schema object or boolean. Retrieval URIs must identify distinct documents
// after URI normalization. The validation owns its registry and never fetches
// missing documents. It does not modify the schema, value, or resources.
func ValidateJSONSchemaValueWithResources(schema map[string]any, value any, resources map[string]json.RawMessage) error {
	v := newToolSchemaValidator(schema)
	r := v.resourceIndex()
	if err := r.registerExternal(resources); err != nil {
		return err
	}
	// A lazily loaded document can introduce a dynamic reference not present in
	// the root. Track the resource stack from the start when using a registry.
	v.trackDynamic = r.hasDynamicRef || len(r.external) > 0
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
	r.orders = make(map[uintptr][]string)
	r.readOrder(resource.schema, resource.raw)
	r.index(resource.schema, document)
	r.orders = nil
	delete(r.external, document)
	return resource.schema, true
}
