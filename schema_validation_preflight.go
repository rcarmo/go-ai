package goai

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// This prototype is intentionally not called by ValidateToolArguments. It
// describes only plain 2020-12 compiler reachability; annotation and dynamic
// scope, diagnostics and host-engine equivalence require separate work.
var errSchemaPatternPreflightUnsupported = errors.New("schema pattern preflight unsupported")

const (
	maxSchemaPatternNodes    = 16384
	maxSchemaPatternDepth    = 256
	maxSchemaPatternValues   = 65536
	maxSchemaPatternRawBytes = 4 << 20
)

type plainPatternTraversal struct {
	validator *toolSchemaValidator
	resources *toolSchemaResources
	seen      map[uintptr]bool
	nodes     int
}

func (v *toolSchemaValidator) checkPlainSchemaPatterns() error {
	// Indexing reads all schema-bearing nodes and the raw JSON order, including
	// unused definitions. Bound that work before invoking the existing indexer.
	if len(v.schemaRaw) > maxSchemaPatternRawBytes {
		return fmt.Errorf("%w: raw size limit", errSchemaPatternPreflightUnsupported)
	}
	if err := boundSchemaPatternInput(v.root); err != nil {
		return err
	}
	p := &plainPatternTraversal{validator: v, resources: v.resourceIndex(), seen: make(map[uintptr]bool)}
	return p.walk(v.root, 0)
}

// Bound every container and scalar, including ignored annotations, because
// readOrder traverses the entire JSON document before index() walks schemas.
// This also rejects cycles in private non-JSON inputs without recursion.
func boundSchemaPatternInput(root interface{}) error {
	type frame struct {
		value interface{}
		depth int
	}
	stack := []frame{{root, 0}}
	// The public input is JSON, a tree. Private alias/cycle inputs are outside
	// this prototype: reject repeated containers before the legacy indexer can
	// recursively follow a cyclic const or enum value.
	seenMaps := make(map[uintptr]bool)
	seenSlices := make(map[uintptr]bool)
	count := 0
	bytes := 0
	addBytes := func(n int) bool {
		if n > maxSchemaPatternRawBytes-bytes {
			return false
		}
		bytes += n
		return true
	}
	for len(stack) != 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		count++
		if count > maxSchemaPatternValues || current.depth > maxSchemaPatternDepth {
			return fmt.Errorf("%w: input work limit", errSchemaPatternPreflightUnsupported)
		}
		switch value := current.value.(type) {
		case string:
			if !addBytes(len(value)) {
				return fmt.Errorf("%w: input size limit", errSchemaPatternPreflightUnsupported)
			}
		case json.Number:
			if !addBytes(len(value)) {
				return fmt.Errorf("%w: input size limit", errSchemaPatternPreflightUnsupported)
			}
		case map[string]interface{}:
			id := reflect.ValueOf(value).Pointer()
			if seenMaps[id] {
				return fmt.Errorf("%w: aliased or cyclic input", errSchemaPatternPreflightUnsupported)
			}
			seenMaps[id] = true
			if len(value) > maxSchemaPatternValues-count-len(stack) {
				return fmt.Errorf("%w: input work limit", errSchemaPatternPreflightUnsupported)
			}
			for key, child := range value {
				if !addBytes(len(key)) {
					return fmt.Errorf("%w: input size limit", errSchemaPatternPreflightUnsupported)
				}
				stack = append(stack, frame{child, current.depth + 1})
			}
		case []interface{}:
			if len(value) == 0 {
				continue
			}
			id := reflect.ValueOf(value).Pointer()
			if seenSlices[id] {
				return fmt.Errorf("%w: aliased or cyclic input", errSchemaPatternPreflightUnsupported)
			}
			seenSlices[id] = true
			if len(value) > maxSchemaPatternValues-count-len(stack) {
				return fmt.Errorf("%w: input work limit", errSchemaPatternPreflightUnsupported)
			}
			for _, child := range value {
				stack = append(stack, frame{child, current.depth + 1})
			}
		}
	}
	return nil
}

func (p *plainPatternTraversal) walk(raw interface{}, depth int) error {
	if depth > maxSchemaPatternDepth {
		return fmt.Errorf("%w: depth limit", errSchemaPatternPreflightUnsupported)
	}
	if _, ok := raw.(bool); ok {
		return nil
	}
	node, ok := raw.(map[string]interface{})
	if !ok || node == nil {
		return fmt.Errorf("%w: non-schema value", errSchemaPatternPreflightUnsupported)
	}
	id := reflect.ValueOf(node).Pointer()
	if p.seen[id] {
		return nil
	}
	p.nodes++
	if p.nodes > maxSchemaPatternNodes {
		return fmt.Errorf("%w: node limit", errSchemaPatternPreflightUnsupported)
	}
	p.seen[id] = true
	if _, ok := node["unevaluatedProperties"]; ok {
		return fmt.Errorf("%w: annotations", errSchemaPatternPreflightUnsupported)
	}
	if _, ok := node["unevaluatedItems"]; ok {
		return fmt.Errorf("%w: annotations", errSchemaPatternPreflightUnsupported)
	}
	if _, ok := node["$dynamicRef"].(string); ok {
		return fmt.Errorf("%w: dynamic reference", errSchemaPatternPreflightUnsupported)
	}
	typeName, _ := node["type"].(string)
	bytesOnly := typeName == "bytes"
	if types, ok := node["type"].([]interface{}); ok && len(types) == 1 {
		typeName, _ = types[0].(string)
		bytesOnly = typeName == "bytes"
	}
	if !bytesOnly {
		if pattern, ok := node["pattern"].(string); ok {
			if err := p.validator.checkSchemaPatternSyntax(pattern); err != nil {
				return err
			}
		}
		if err := p.object(node, depth); err != nil {
			return err
		}
		if err := p.array(node, depth); err != nil {
			return err
		}
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		if values, ok := node[key].([]interface{}); ok {
			for _, value := range values {
				if err := p.walk(value, depth+1); err != nil {
					return err
				}
			}
		}
	}
	if raw, ok := node["not"]; ok {
		if err := p.walk(raw, depth+1); err != nil {
			return err
		}
	}
	if condition, ok := node["if"]; ok {
		then, hasThen := node["then"]
		otherwise, hasElse := node["else"]
		if hasThen || hasElse {
			if hasThen {
				if err := p.walk(then, depth+1); err != nil {
					return err
				}
			}
			if hasElse {
				if err := p.walk(otherwise, depth+1); err != nil {
					return err
				}
			}
			if err := p.walk(condition, depth+1); err != nil {
				return err
			}
		}
	}
	if ref, ok := node["$ref"].(string); ok {
		// With unresolvedRefs: "reject", an unresolved target is false.
		if target := p.validator.resolve(ref, node); target != nil {
			if err := p.walk(target, depth+1); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *plainPatternTraversal) object(node map[string]interface{}, depth int) error {
	if properties, ok := node["properties"].(map[string]interface{}); ok {
		for _, key := range p.resources.keys(properties) {
			if err := p.walk(properties[key], depth+1); err != nil {
				return err
			}
		}
	}
	if patterns, ok := node["patternProperties"].(map[string]interface{}); ok {
		for _, key := range p.resources.keys(patterns) {
			if err := p.validator.checkSchemaPatternSyntax(key); err != nil {
				return err
			}
			if err := p.walk(patterns[key], depth+1); err != nil {
				return err
			}
		}
	}
	if extra, ok := node["additionalProperties"]; ok {
		if yes, isBool := extra.(bool); !isBool || !yes {
			if err := p.walk(extra, depth+1); err != nil {
				return err
			}
		}
	}
	if names, ok := node["propertyNames"]; ok {
		if err := p.walk(names, depth+1); err != nil {
			return err
		}
	}
	for _, keyword := range []string{"dependentSchemas", "dependencies"} {
		if entries, ok := node[keyword].(map[string]interface{}); ok {
			for _, key := range p.resources.keys(entries) {
				value := entries[key]
				if _, list := value.([]interface{}); keyword == "dependencies" && list {
					continue
				}
				if err := p.walk(value, depth+1); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (p *plainPatternTraversal) array(node map[string]interface{}, depth int) error {
	items, hasItems := node["items"]
	var prefix []interface{}
	var rest interface{}
	hasRest := false
	if tuple, ok := items.([]interface{}); ok {
		prefix = tuple
		rest, hasRest = node["additionalItems"]
	} else {
		prefix, _ = node["prefixItems"].([]interface{})
		rest, hasRest = items, hasItems
	}
	for _, value := range prefix {
		if err := p.walk(value, depth+1); err != nil {
			return err
		}
	}
	if hasRest {
		if yes, isBool := rest.(bool); !isBool || !yes {
			if err := p.walk(rest, depth+1); err != nil {
				return err
			}
		}
	}
	if contains, ok := node["contains"]; ok {
		if err := p.walk(contains, depth+1); err != nil {
			return err
		}
	}
	return nil
}
