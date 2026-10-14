package quickjs

import "strings"

// ReservedGlobalRoot excludes intrinsic and ambient-authority names from
// host injection. Script helpers/models are checked by the durable layer.
func ReservedGlobalRoot(name string) bool {
	root, _, _ := strings.Cut(name, ".")
	switch root {
	case "globalThis", "undefined", "NaN", "Infinity", "Object", "Function", "Array", "Number", "Boolean", "String", "Symbol", "BigInt", "Date", "RegExp", "Math", "JSON", "Reflect", "Proxy", "Promise", "Map", "Set", "WeakMap", "WeakSet", "WeakRef", "FinalizationRegistry", "Error", "EvalError", "RangeError", "ReferenceError", "SyntaxError", "TypeError", "URIError", "AggregateError", "ArrayBuffer", "SharedArrayBuffer", "DataView", "Atomics", "Int8Array", "Uint8Array", "Uint8ClampedArray", "Int16Array", "Uint16Array", "Int32Array", "Uint32Array", "Float16Array", "Float32Array", "Float64Array", "BigInt64Array", "BigUint64Array", "Intl", "eval", "parseInt", "parseFloat", "isNaN", "isFinite", "decodeURI", "decodeURIComponent", "encodeURI", "encodeURIComponent", "escape", "unescape", "process", "require", "fetch", "XMLHttpRequest", "WebSocket", "setTimeout", "setInterval", "clearTimeout", "clearInterval", "queueMicrotask", "std", "os", "__proto__", "constructor", "prototype":
		return true
	}
	return false
}
