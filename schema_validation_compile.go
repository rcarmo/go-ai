package goai

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
)

// JSONSchemaCompileOptions configures standalone schema preparation. By
// default, unresolved references reached during preparation fail it. With
// RejectUnresolvedRefs, they instead reject the value at their location.
type JSONSchemaCompileOptions struct {
	JSONSchemaCheckOptions
	RejectUnresolvedRefs bool
}

// CompiledJSONSchema is a prepared schema for repeated checks, explanations
// and coercion. Its schema and host resource registry belong to it; each call
// starts with fresh value and dynamic scope state.
type CompiledJSONSchema struct {
	schema   any
	prepared *toolSchemaValidator
}

// CompileJSONSchema prepares a schema without a value and resolves references
// reached by its validation keywords. It does not fetch documents. Definitions
// and unused host documents do not run as schemas unless referenced. Dynamic
// references resolve their initial targets here; runtime scope selects their
// actual targets. This is not a full JSON Schema meta-schema check.
func CompileJSONSchema(schema json.RawMessage, options JSONSchemaCompileOptions) (*CompiledJSONSchema, error) {
	var drafts []JSONSchemaReferenceDraft
	if options.Draft != "" {
		drafts = []JSONSchemaReferenceDraft{options.Draft}
	}
	call, err := prepareStandaloneSchemaValue(schema, nil, options.Resources, drafts)
	if err != nil {
		return nil, err
	}
	v := call.validator
	v.formatPolicy = configuredSchemaFormats(options.AssertFormats)
	if err := v.preflightSchemaReferences(call.schema, options.RejectUnresolvedRefs); err != nil {
		return nil, err
	}
	return &CompiledJSONSchema{schema: call.schema, prepared: v}, nil
}

// Check reports whether value satisfies the schema without coercion. Pass
// a decoded JSON value or json.RawMessage. Native values may also use typed
// []byte for bytes, non-nil *big.Int for bigint, and SchemaUndefined for an
// explicit undefined.
func (c *CompiledJSONSchema) Check(value any) (bool, error) {
	v, value, _, err := c.call(value)
	if err != nil {
		return false, err
	}
	return v.checkStandaloneValue(c.schema, value)
}

// Explain returns structured diagnostics, or an empty slice for a valid
// value. A json.RawMessage preserves the original order of instance keys.
func (c *CompiledJSONSchema) Explain(value any) ([]JSONSchemaDiagnostic, error) {
	v, value, raw, err := c.call(value)
	if err != nil {
		return nil, err
	}
	return v.explainSchemaValue(c.schema, value, raw)
}

// Validate checks a value without coercion. It returns the supplied value
// with an empty diagnostic slice, or nil with the diagnostics of the invalid
// value. A json.RawMessage is decoded before checking. Errors report input
// or checker failures.
func (c *CompiledJSONSchema) Validate(value any) (any, []JSONSchemaDiagnostic, error) {
	v, value, raw, err := c.call(value)
	if err != nil {
		return nil, nil, err
	}
	return (standaloneSchemaValue{validator: v, schema: c.schema, value: value, valueRaw: raw}).validate()
}

// Coerce applies the coercion rules without requiring the result to pass
// validation or modifying the input. For an explicit native input, an
// undefined schema may produce SchemaUndefined. A json.RawMessage does not
// opt into that non-JSON conversion.
func (c *CompiledJSONSchema) Coerce(value any) (any, error) {
	call, err := c.coercingInput(value)
	if err != nil {
		return nil, err
	}
	return call.coerce()
}

// Parse coerces a value and checks the result. It returns the coerced value
// with an empty diagnostic slice, or nil with the diagnostics of the coerced
// value. Input domains follow Coerce. Errors report input or checker failures.
func (c *CompiledJSONSchema) Parse(value any) (any, []JSONSchemaDiagnostic, error) {
	call, err := c.coercingInput(value)
	if err != nil {
		return nil, nil, err
	}
	return call.parse()
}

func (c *CompiledJSONSchema) coercingInput(value any) (standaloneSchemaValue, error) {
	v, value, raw, err := c.call(value)
	if err != nil {
		return standaloneSchemaValue{}, err
	}
	v.coerceUndefined = raw == nil
	return standaloneSchemaValue{validator: v, schema: c.schema, value: value}, nil
}

func (c *CompiledJSONSchema) call(value any) (*toolSchemaValidator, any, json.RawMessage, error) {
	if c == nil || c.prepared == nil {
		return nil, nil, nil, fmt.Errorf("schema is not compiled")
	}
	value, raw, err := standaloneSchemaInput(value)
	if err != nil {
		return nil, nil, nil, err
	}
	p := c.prepared
	v := newToolSchemaValidator(p.root)
	v.draft7, v.trackDynamic, v.formatPolicy = p.draft7, p.trackDynamic, p.formatPolicy
	v.resources = p.resources
	v.refs, v.refBases = maps.Clone(p.refs), maps.Clone(p.refBases)
	if r := p.resources; len(r.external) > 0 {
		// Only loading a previously unused document can extend this index.
		// Fork its maps before evaluation, never its immutable schema objects.
		fork := *r
		fork.bases = maps.Clone(r.bases)
		fork.documents = maps.Clone(r.documents)
		fork.external = maps.Clone(r.external)
		fork.anchors = maps.Clone(r.anchors)
		fork.dynamicAnchors = maps.Clone(r.dynamicAnchors)
		fork.compilerOrders = maps.Clone(r.compilerOrders)
		fork.literalOrders = maps.Clone(r.literalOrders)
		v.resources = &fork
	}
	return v, value, raw, nil
}

type schemaPreflightVisit struct {
	node       uintptr
	base       string
	annotating bool
}

func (v *toolSchemaValidator) preflightSchemaReferences(root any, reject bool) error {
	r := v.resourceIndex()
	seen := make(map[schemaPreflightVisit]bool)
	var walk func(any, string, bool) error
	walk = func(raw any, base string, annotating bool) error {
		node, ok := raw.(map[string]any)
		if !ok {
			if _, boolean := raw.(bool); boolean {
				return nil
			}
			return fmt.Errorf("schema must be an object or a boolean")
		}
		id := reflect.ValueOf(node).Pointer()
		if indexed, ok := r.bases[id]; ok {
			base = indexed
		}
		_, unevaluatedProperties := node["unevaluatedProperties"]
		_, unevaluatedItems := node["unevaluatedItems"]
		annotating = !v.draft7 && (annotating || unevaluatedProperties || unevaluatedItems)
		visit := schemaPreflightVisit{id, base, annotating}
		if seen[visit] {
			return nil
		}
		seen[visit] = true

		follow := func(ref string) error {
			target, targetBase := v.resolveFromBase(ref, base)
			if target == nil {
				if reject {
					return nil
				}
				//lint:ignore ST1005 Keep the pinned schema setup error prefix.
				return fmt.Errorf("Unresolved $ref %s", ref)
			}
			return walk(target, targetBase, annotating)
		}
		if ref, ok := node["$ref"].(string); ok {
			if err := follow(ref); err != nil || v.draft7 {
				return err
			}
		}
		if ref, ok := node["$dynamicRef"].(string); ok && !v.draft7 {
			if err := follow(ref); err != nil {
				return err
			}
		}

		// Definitions and annotations are indexed, not executable children.
		// Member and item constraints run on a different value and start a
		// fresh annotation scope. In-place applicators inherit this scope.
		for _, key := range []string{"properties", "patternProperties", "dependencies", "dependentSchemas"} {
			if v.draft7 && key == "dependentSchemas" {
				continue
			}
			children, _ := node[key].(map[string]any)
			for _, name := range r.keys(children) {
				child := children[name]
				if _, required := child.([]any); key == "dependencies" && required {
					continue
				}
				if err := walk(child, base, annotating && key == "dependentSchemas"); err != nil {
					return err
				}
			}
		}
		for _, key := range []string{"additionalProperties", "propertyNames", "contains", "not", "unevaluatedProperties", "unevaluatedItems"} {
			if v.draft7 && (key == "unevaluatedProperties" || key == "unevaluatedItems") {
				continue
			}
			if child, ok := node[key]; ok {
				if err := walk(child, base, false); err != nil {
					return err
				}
			}
		}
		prefix, tuples := node["items"].([]any)
		rest := "additionalItems"
		if !tuples {
			if !v.draft7 {
				prefix, _ = node["prefixItems"].([]any)
			}
			rest = "items"
		}
		for _, child := range prefix {
			if err := walk(child, base, false); err != nil {
				return err
			}
		}
		if child, ok := node[rest]; ok {
			if err := walk(child, base, false); err != nil {
				return err
			}
		}
		for _, key := range []string{"allOf", "anyOf", "oneOf"} {
			children, _ := node[key].([]any)
			for _, child := range children {
				if err := walk(child, base, annotating); err != nil {
					return err
				}
			}
		}
		if _, condition := node["if"]; condition {
			_, hasThen := node["then"]
			_, hasElse := node["else"]
			if annotating || hasThen || hasElse {
				for _, key := range []string{"if", "then", "else"} {
					if child, ok := node[key]; ok {
						if err := walk(child, base, annotating); err != nil {
							return err
						}
					}
				}
			}
		}
		return nil
	}
	return walk(root, toolSchemaDefaultBase, false)
}
