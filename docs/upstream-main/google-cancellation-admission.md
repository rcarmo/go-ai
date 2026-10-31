## Google cancellation and JSON syntax admission

Pinned target: `81f3cd412758ebdd92324c7d63b669a189408d0a`. Both adapters classify caught failures as aborted whenever the caller signal is aborted and check that signal before successful completion. `cancellation_main_test.go` translates this into ten registered production HTTP/SSE cases, five per adapter:

| Scenario | Expected contract | Baseline 7408623 |
| --- | --- | --- |
| Cancel before HTTP | One aborted terminal with assistant metadata/usage/diagnostic; no HTTP or hook invocation | Missing assistant payload |
| Cancel after partial content | One aborted terminal retaining text and first response ID | Error terminal with pending payload |
| Cancel and reject the next hook | Aborted terminal retaining the hook diagnostic and earlier content | Error instead of aborted |
| Cancel while processing a finish chunk | Aborted terminal, never successful done | Error terminal with stop payload |
| Reject hook without cancelling | Error terminal; request context remains active | Pass |

Eight cases fail before production changes; both uncancelled controls pass. Parked server responses must be released before test cleanup cancels the caller context. The uncancelled controls specifically check the transport's early-return body closure. The ten cases pass in twenty repetitions and in the Google race suite.

The production path now passes its request context into the parser. Parser cancellation remains local as well, so early hook failure cannot leave a channel producer blocked. A common failure path updates terminal and assistant reasons together, sets the diagnostic and retains accumulated output. Cancellation is checked before done even when a finish reason was already seen. HTTP-request cancellation now includes an assistant carrier rather than a bare error event. The transport owns and closes the body; the legacy reader helper keeps caller ownership. Scanner bounds, typed/lossless authority and detached snapshots are unchanged.

Go request/read cancellation diagnostics use native context/HTTP errors; their byte strings are not claimed to equal SDK-specific exceptions. The hook-rejection diagnostic is preserved exactly. Deadline variants, concurrent cancellation races, other preflight validation/error paths and timestamp/duration parity need further review.

## SDK-backed syntax reference

The pinned manifest/lockfile select `@google/genai` 2.21.0. Its exact registry archive was fetched read-only and checked against the lockfile SHA-512 integrity. No package installation, install script or auth implementation ran. In `dist/node/index.mjs`, both `generateContentStream` branches await `HttpResponse.json()` before transforming/yielding the response. That method delegates to `Response.json()`.

A probe extracted the actual inspected `HttpResponse` class and executed its parser with `{`, `{"candidates":[],}` and `not-json`; all reject. An empty object succeeds. This verifies syntax admission, not full SDK transport, auth or shape conversion. The archive and extracted source were removed after analysis.

`syntax_admission_main_test.go` adds eight native HTTP/SSE cases: those three malformed payloads plus the valid-object control on each adapter. A preceding text chunk must survive; malformed JSON must emit one error, never invoke its observer or accept a later STOP. All six malformed cases failed on the cancellation-fixed intermediate implementation because syntax errors were silently skipped; both controls passed. Syntax errors now terminate, and all eight cases pass. Go parse-error wording is intentionally language-specific; tests require a nonempty diagnostic rather than identical JavaScript exception text.

## Remaining admission and SDK shape work

Valid JSON with unsupported native DTO types still follows the existing typed admission path; no generic observer map is promoted into authority. The SDK transformations are not equivalent to the raw wire projection: they select/transform fields and attach `sdkHttpResponse.headers`. Callback shape/enrichment, SDK reference identity and mutation remain unresolved. Numeric/coercion and non-object inputs need their own reference tests without weakening native domains.

The inspected SDK also rejects a nonblank unfinished EOF buffer and attempts JSON parsing of a `[DONE]` data payload. Native shared SSE parsing flushes at EOF and Google currently skips `[DONE]`; these are concrete framing follow-ups requiring tests and provider-specific treatment without breaking Mistral's EOF contract. Live credentials, Vertex API-key/ADC tests, provider-only options and broader dependency/convergence review remain open.

## Development checks

With `scripts/project-env.sh`, Go 1.27.3 and `GOMAXPROCS=4`: final full root normal tests, Google normal/race tests, root vet, scoped staticcheck v0.8.2 and `git diff --check` pass. Disposable SDK probe files and run logs were deleted. Root staticcheck's 23 previously recorded findings remain open. No acceptance or profiling claim; native v1.4.0, generated catalogues and Go dependencies are unchanged.
