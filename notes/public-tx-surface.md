# Public transaction surface

Target: official pi-durable1.5.0 at f9a26365568127461c47a07a37dcb42c140a3ac5, types.ts987-1085. The public Tx interface is smaller than the internal Transaction class. In particular, setTask is internal. This port must not add a public SetTask replacement for native raw task mutation.

Nine native-only Tx methods are now private: MintID, PutTask, PutSubmission, CreateDocument, Document, Unsupported, ScanSubmissions, CopyDocument and Submission. Storage/Session/Harness methods with the same names are unchanged by this slice. Public callers use the canonical draft/definition APIs; private bootstrap, runtime and historical native fixtures retain their existing low-level operations. No public compatibility aliases remain for these nine methods.

The public Go surface has20 methods corresponding to the source interface. Go splits the typed entry overloads into TypedEntry/AppendTypedEntry and spells doc/retireDoc as the existing AcquireDocument/RetireDefinition methods. This is a structural mapping, not acceptance of every overload, seed type, lifecycle or record-identity behaviour.

Public LatestHeadMarker now accepts only the conversation and reads its current committed history. The old optional historical ceiling is private. A real SQLite test appends two self-headed entries and checks that the public call returns the newest one. Internal historical readers keep their old ceiling semantics.

## Test-first refactor and audit

The public method-set control first failed0.022s on all nine extra exports; the one-argument marker control failed0.055s against the old signature. Both pass after the refactor. Ten method-set repetitions pass0.016s and ten shuffled marker repetitions pass0.352s; vet/Staticcheck/logging pass. Full repository ./... passes26 packages: root0.853s, durable585.852s/tools28.155s and public consumers12.711s. Nine independently reintroduced public API aliases plus marker arity/current-value faults are detected after compilation. Ten surface repetitions0.016s/ten shuffled marker repetitions0.352s, vet/Staticcheck/logging/register and selected original277 (200 unchanged +77 probes) pass, zero original skips. Analysed temporary scripts/logs/overlays are removed. This establishes the tested public Tx method boundary and current-marker shape/value, not complete API/record identity/internal mutation/document/builtin/Storage/release conformance.

A standard-Go type checker identifies the actual Tx method objects, including method expressions and values. It renames578 net declarations/selectors in155 files, leaving same-named Storage/Session/Harness operations untouched. A second pass type-checks the original pushed tree and verifies153 files are byte-equivalent after only the exact identifier migration. The public current-marker adapter and added source controls are the two explicit semantic-file exceptions. The initial mechanical pass also renamed the new marker test's method-value selector; it was restored to exercise the public method before verification.

The selected source interface is a static contract. Original JavaScript Transaction instances can also have internal class methods; runtime prototype enumeration is not a test of public Tx visibility. The reference marker probe checks the current one-argument behaviour independently in selected277, zero skipped. Retained native tests still call private low-level operations and do not establish canonical public API semantics.

## Remaining scope

Internal canonical task replacement and its atomic ownership/validation ordering remain open. This slice does not align every other public type or native-extra method outside Tx, document/family overloads, optional values/errors, read-key/order/lifecycle behaviour, builtin execution, Storage, record identity or release profiling. Full alignment and release acceptance remain incomplete.
