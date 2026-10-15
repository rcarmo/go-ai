package quickjs

import (
	"encoding/json"
	"errors"
	"unicode/utf8"
)

// validateInputs bounds native setup before instantiation or JSON expansion.
// These byte budgets are native limits, separate from guest UTF16 store limits.
func validateInputs(code string, opts Options) error {
	invalid := func() error { return errors.New("invalid or oversized QuickJS setup input") }
	if !utf8.ValidString(code) || len(code) > int(maxBridgeStringBytes) || opts.MemoryLimitBytes < 0 || opts.StartedAt < 0 {
		return invalid()
	}
	if len(opts.Tools)+len(opts.Globals) > maxBridgeSetupItems || len(opts.Store) > maxBridgeSetupItems {
		return invalid()
	}
	total := 0
	add := func(value string) bool {
		if !utf8.ValidString(value) || len(value) > int(maxBridgeStringBytes) || len(value) > maxBridgeOutputBytes-total {
			return false
		}
		total += len(value)
		return true
	}
	aliases := map[string]string{}
	for _, tool := range opts.Tools {
		identifier := tool.JSName
		if identifier == "" {
			identifier = Identifier(tool.Name)
		}
		if tool.Name == "" || Identifier(identifier) != identifier || tool.Execute == nil || !add(tool.Name) || !add(identifier) || !add(tool.Description) {
			return invalid()
		}
		for _, alias := range []string{tool.Name, identifier} {
			if previous, ok := aliases[alias]; ok && previous != tool.Name {
				return invalid()
			}
			aliases[alias] = tool.Name
		}
	}
	globals := map[string]bool{}
	for _, global := range opts.Globals {
		if global.Name == "" || ReservedGlobalRoot(global.Name) || globals[global.Name] || global.Execute == nil || !add(global.Name) {
			return invalid()
		}
		globals[global.Name] = true
	}
	for key, value := range opts.Store {
		if !add(key) || len(value) > int(maxBridgeStringBytes) || !json.Valid(value) || !add(string(value)) {
			return invalid()
		}
	}
	return nil
}
