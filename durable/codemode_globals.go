package durable

import (
	"context"
	"github.com/rcarmo/go-ai/internal/quickjs"
	"strings"
)

// CodemodeGlobal is process-local host authority. Version pins callback
// semantics; change it whenever replay-visible behaviour changes. Execute may
// run again when its delivery commit is unconfirmed, so callbacks must be pure
// or externally idempotent. Use nested tools for durably settled effects.
type CodemodeGlobal struct {
	Name    string
	Version int
	Spread  bool
	Execute func(context.Context, any) (any, error)
}
type CodemodeGlobalDescription struct {
	Name    string `json:"name"`
	Version int    `json:"version"`
	Spread  bool   `json:"spread"`
}

func validateCodemodeGlobals(globals []CodemodeGlobal, l Limits) error {
	descriptions := make([]CodemodeGlobalDescription, 0, len(globals))
	for _, global := range globals {
		if global.Execute == nil {
			return reject("nil codemode global callback")
		}
		descriptions = append(descriptions, CodemodeGlobalDescription{Name: global.Name, Version: global.Version, Spread: global.Spread})
	}
	return validateCodemodeGlobalDescriptions(descriptions, l)
}
func validateCodemodeGlobalDescriptions(globals []CodemodeGlobalDescription, l Limits) error {
	if len(globals) > l.MaxPage {
		return reject("codemode global budget")
	}
	names := map[string]bool{}
	reserved := map[string]bool{"tools": true, "ALL_TOOLS": true, "console": true, "text": true, "image": true, "exit": true, "store": true, "load": true, "models": true, "searchTools": true, "describeTool": true, "describeNamespace": true}
	for _, global := range globals {
		parts := strings.Split(global.Name, ".")
		if len(parts) > 2 || global.Name == "" || len(global.Name) > l.MaxStringBytes || global.Version < 1 || uint64(global.Version) > MaxID || reserved[parts[0]] || quickjs.ReservedGlobalRoot(global.Name) || names[global.Name] {
			return reject("invalid codemode global")
		}
		for _, part := range parts {
			if part == "" || CodemodeIdentifier(part) != part {
				return reject("invalid codemode global identifier")
			}
		}
		names[global.Name] = true
	}
	for name := range names {
		for other := range names {
			if name != other && strings.HasPrefix(other, name+".") {
				return reject("codemode global namespace collision")
			}
		}
	}
	return nil
}
func codemodeCustomGlobal(start CodemodeStart, name string) *CodemodeGlobalDescription {
	for _, global := range start.Globals {
		if global.Name == name {
			copy := global
			return &copy
		}
	}
	return nil
}
func (r *TaskRuntime) callCodemodeGlobal(ctx context.Context, start CodemodeStart, call CodemodeCall) (value any, err error) {
	pinned := codemodeCustomGlobal(start, call.Name)
	if pinned == nil {
		return nil, reject("unknown codemode global")
	}
	var callback *CodemodeGlobal
	for i := range r.harness.options.CodemodeGlobals {
		global := &r.harness.options.CodemodeGlobals[i]
		if global.Name == call.Name && global.Version == pinned.Version && global.Spread == pinned.Spread {
			callback = global
			break
		}
	}
	if callback == nil {
		return nil, reject("codemode global revoked or changed")
	}
	defer func() {
		if recover() != nil {
			value = nil
			err = reject("codemode global callback panic")
		}
	}()
	// Host code receives an owned value, never an alias into the issued journal.
	arguments, copyErr := copyObject(call.Arguments, r.harness.session.limits)
	if copyErr != nil {
		return nil, copyErr
	}
	return callback.Execute(ctx, arguments["args"])
}
