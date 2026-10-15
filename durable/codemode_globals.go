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
	Description               string
	Signature                 string
	InputSchema, OutputSchema JSON
	Name                      string
	Version                   int
	Spread                    bool
	Execute                   func(context.Context, any) (any, error)
}
type CodemodeGlobalDescription struct {
	Description  string `json:"description,omitempty"`
	Signature    string `json:"signature,omitempty"`
	InputSchema  JSON   `json:"inputSchema,omitempty"`
	OutputSchema JSON   `json:"outputSchema,omitempty"`
	Name         string `json:"name"`
	Version      int    `json:"version"`
	Spread       bool   `json:"spread"`
}

func validateCodemodeGlobals(globals []CodemodeGlobal, l Limits) error {
	descriptions := make([]CodemodeGlobalDescription, 0, len(globals))
	for _, global := range globals {
		if global.Execute == nil {
			return reject("nil codemode global callback")
		}
		descriptions = append(descriptions, codemodeGlobalDescription(global))
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
		if len(parts) > 2 || global.Name == "" || len(global.Name) > l.MaxStringBytes || len(global.Description) > l.MaxStringBytes || len(global.Signature) > l.MaxStringBytes || global.Version < 1 || uint64(global.Version) > MaxID || reserved[parts[0]] || quickjs.ReservedGlobalRoot(global.Name) || names[global.Name] {
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
func (r *TaskRuntime) callCodemodeGlobal(ctx context.Context, start CodemodeStart, call CodemodeCall, candidates ...[]CodemodeGlobal) (value any, err error) {
	pinned := codemodeCustomGlobal(start, call.Name)
	if pinned == nil {
		return nil, reject("unknown codemode global")
	}
	var callback *CodemodeGlobal
	globals := r.harness.options.CodemodeGlobals
	if len(candidates) > 0 {
		globals = candidates[0]
	}
	for i := range globals {
		global := &globals[i]
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

func codemodeGlobalDescription(global CodemodeGlobal) CodemodeGlobalDescription {
	return CodemodeGlobalDescription{Name: global.Name, Version: global.Version, Spread: global.Spread, Description: global.Description, Signature: global.Signature, InputSchema: global.InputSchema, OutputSchema: global.OutputSchema}
}
func cloneCodemodeGlobals(globals []CodemodeGlobal, l Limits) ([]CodemodeGlobal, error) {
	if err := validateCodemodeGlobals(globals, l); err != nil {
		return nil, err
	}
	out := append([]CodemodeGlobal(nil), globals...)
	for i := range out {
		var err error
		out[i].InputSchema, err = copyObjectNullable(out[i].InputSchema, l)
		if err != nil {
			return nil, err
		}
		out[i].OutputSchema, err = copyObjectNullable(out[i].OutputSchema, l)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// CodemodeDeclarations renders only the pinned run snapshot, never a live registry.
func (start CodemodeStart) CodemodeDeclarations() string {
	globals := codemodeBuiltinDeclarations(start.PaidModels)
	for _, global := range start.Globals {
		globals = append(globals, CodemodeGlobalDeclaration{Name: global.Name, Description: global.Description, Signature: global.Signature, InputSchema: global.InputSchema, OutputSchema: global.OutputSchema})
	}
	return RenderCodemodeDeclarations(start.Tools, globals)
}
