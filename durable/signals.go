package durable

import (
	"fmt"
	"maps"
	"slices"
	"strings"
)

// SignalDefinition describes a versioned signal payload and optional migration.
type SignalDefinition struct {
	Name    string
	Version uint64
	Schema  JSON
	Migrate func(value any, fromVersion uint64) (any, error)
}

// SignalKey identifies a delivery or a wait within a signal kind.
type SignalKey struct {
	Signal string `json:"signal"`
	Key    string `json:"key"`
}

// SignalToken is constructed by DefineSignal.
type SignalToken struct{ definition SignalDefinition }

// DefineSignal defines a versioned value that can be delivered to a task.
func DefineSignal(def SignalDefinition) (*SignalToken, error) {
	if def.Name == "" || strings.Contains(def.Name, "/") {
		//lint:ignore ST1005 exact upstream diagnostic
		return nil, fmt.Errorf("Signal name %q must be non-empty, without \"/\"", def.Name)
	}
	if def.Version == 0 {
		//lint:ignore ST1005 exact upstream diagnostic
		return nil, fmt.Errorf("Signal %s version must be a positive integer", def.Name)
	}
	schema, err := copyObject(def.Schema, DefaultLimits())
	if err != nil {
		return nil, err
	}
	if invalid := signalNonJSONType(map[string]any(schema)); invalid != "" {
		//lint:ignore ST1005 exact upstream diagnostic
		return nil, fmt.Errorf("Signal %s schema admits non-JSON values (%s)", def.Name, invalid)
	}
	def.Schema = schema
	return &SignalToken{definition: def}, nil
}

// Name returns the kind of the signal.
func (token *SignalToken) Name() string { return token.definition.Name }

// Version returns the schema version written in a delivery.
func (token *SignalToken) Version() uint64 { return token.definition.Version }

// Key constructs a key for a nonempty name.
func (token *SignalToken) Key(key string) (SignalKey, error) {
	if token == nil || token.definition.Schema == nil {
		return SignalKey{}, reject("invalid signal token")
	}
	if key == "" {
		//lint:ignore ST1005 exact upstream diagnostic
		return SignalKey{}, fmt.Errorf("Signal %s key must be non-empty", token.Name())
	}
	return SignalKey{Signal: token.Name(), Key: key}, nil
}

// Follow schema-bearing keywords only. An annotation can mention a non-JSON
// type without the schema admitting values of that type.
func signalNonJSONType(raw any) string {
	node, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	types, ok := node["type"].([]any)
	if !ok {
		types = []any{node["type"]}
	}
	for _, raw := range types {
		kind, _ := raw.(string)
		switch kind {
		case "bytes", "bigint", "undefined":
			return kind
		}
	}
	for _, key := range [...]string{
		"items", "prefixItems", "additionalItems", "unevaluatedItems", "contains",
		"anyOf", "oneOf", "allOf", "not", "if", "then", "else",
		"additionalProperties", "unevaluatedProperties", "propertyNames",
	} {
		switch child := node[key].(type) {
		case []any:
			for _, value := range child {
				if kind := signalNonJSONType(value); kind != "" {
					return kind
				}
			}
		default:
			if kind := signalNonJSONType(child); kind != "" {
				return kind
			}
		}
	}
	for _, key := range [...]string{"properties", "patternProperties", "dependentSchemas", "dependencies", "$defs", "definitions"} {
		children, _ := node[key].(map[string]any)
		for _, name := range slices.Sorted(maps.Keys(children)) {
			if kind := signalNonJSONType(children[name]); kind != "" {
				return kind
			}
		}
	}
	return ""
}
