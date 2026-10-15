package quickjs

import (
	"crypto/sha256"
	"fmt"
)

// Identity pins guest assets and native host semantics, not generated outputs.
// Bump host protocol when replay-visible host behaviour changes.
var runtimeIdentity = func() string {
	return fmt.Sprintf("quickjs-wasi/3.6.2;wazero/1.12.0;host/49;wasm/%x;prelude/%x", sha256.Sum256(quickjsWasm), sha256.Sum256([]byte(preludeSource)))
}()

func Identity() string { return runtimeIdentity }
