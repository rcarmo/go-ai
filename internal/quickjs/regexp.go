package quickjs

import (
	"context"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

// CheckRegExp checks ECMAScript Unicode-mode syntax without running a match.
// Each call owns a fresh module and bounded heap. Only this constant function
// is evaluated: pattern is a JS string argument, never interpolated code. No
// codemode prelude, tools, globals, files or network capabilities are installed.
// Constructor exceptions return false, as in the upstream format check;
// initialization, cancellation and WASM failures remain distinct errors.
// The caller controls the deadline; schema validation supplies five seconds.
func (e *Engine) CheckRegExp(ctx context.Context, pattern string) (valid bool, err error) {
	if ctx == nil {
		return false, fmt.Errorf("nil regexp context")
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if e == nil || e.runtime == nil {
		return false, fmt.Errorf("nil regexp engine")
	}
	defer func() {
		if ctx.Err() != nil {
			valid, err = false, ctx.Err()
		}
	}()
	v := &vm{fn: make(map[string]api.Function)}
	wasmCtx := context.WithValue(ctx, ctxKey{}, v)
	mod, err := e.runtime.InstantiateModule(wasmCtx, e.compiled, wazero.NewModuleConfig().WithName("").WithStartFunctions())
	if err != nil {
		return false, fmt.Errorf("start regexp compiler: %w", err)
	}
	defer mod.Close(context.Background())
	v.mod, v.mem = mod, mod.Memory()
	for _, init := range []struct {
		name string
		args []uint64
	}{
		{"_initialize", nil}, {"qjs_init", nil},
		{"qjs_set_memory_limit", []uint64{32 << 20}},
		{"qjs_set_max_stack_size", []uint64{maxStackSize}},
		{"qjs_set_interrupt_handler", []uint64{1}},
	} {
		n, err := v.call(wasmCtx, init.name, init.args...)
		if err != nil {
			return false, fmt.Errorf("regexp %s: %w", init.name, err)
		}
		if init.name == "qjs_init" && n != 0 {
			return false, fmt.Errorf("regexp initialization failed: %d", n)
		}
	}
	checked := func(h uint32) error {
		exception, err := v.call(wasmCtx, "qjs_is_exception", uint64(h))
		if err != nil {
			return err
		}
		if exception != 0 {
			return fmt.Errorf("regexp compiler initialization or execution failed")
		}
		return nil
	}
	const code = `(pattern => { try { new RegExp(pattern, "u"); return true; } catch { return false; } })`
	codePtr, err := v.writeBytes(wasmCtx, []byte(code))
	if err != nil {
		return false, err
	}
	defer v.call(context.Background(), "wasm_free", uint64(codePtr))
	namePtr, err := v.writeBytes(wasmCtx, []byte("regexp-format.js"))
	if err != nil {
		return false, err
	}
	defer v.call(context.Background(), "wasm_free", uint64(namePtr))
	fn, err := v.call(wasmCtx, "qjs_eval", uint64(codePtr), uint64(len(code)), uint64(namePtr), 0)
	if err != nil {
		return false, err
	}
	defer v.free(uint32(fn))
	if err := checked(uint32(fn)); err != nil {
		return false, err
	}
	argument, err := v.newString(wasmCtx, pattern)
	if err != nil {
		return false, err
	}
	defer v.free(argument)
	if err := checked(argument); err != nil {
		return false, err
	}
	undefined, err := v.call(wasmCtx, "qjs_get_undefined")
	if err != nil {
		return false, err
	}
	defer v.free(uint32(undefined))
	argv, err := v.call(wasmCtx, "wasm_malloc", 4)
	if err != nil {
		return false, err
	}
	if argv == 0 || !v.mem.WriteUint32Le(uint32(argv), argument) {
		return false, fmt.Errorf("regexp argument allocation failed")
	}
	defer v.call(context.Background(), "wasm_free", argv)
	result, err := v.call(wasmCtx, "qjs_call", fn, undefined, 1, argv)
	if err != nil {
		return false, err
	}
	defer v.free(uint32(result))
	if err := checked(uint32(result)); err != nil {
		return false, err
	}
	value, err := v.call(wasmCtx, "qjs_get_bool", result)
	if err != nil {
		return false, err
	}
	return value != 0, nil
}
