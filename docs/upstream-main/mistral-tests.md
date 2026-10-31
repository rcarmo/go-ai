## Mistral upstream test crosswalk

Target: `4b535d57ea8863dbba44b89872146523da271786`. Production baseline: `f84cb57ce35492d0d88e0563b665e3eb7f09b9d9`.

The four pinned Mistral test files expand to 34 cases. Their native assertions pass against unchanged production code. Two cases have the representation limits listed below; they do not establish complete upstream API coverage. No implementation fix followed this baseline run.

### Reasoning mode: 19 cases

Source: `packages/ai/test/mistral-reasoning-mode.test.ts`.
Native: `inference/provider/mistral/options_upstream_main_test.go`, `TestUpstreamMainMistralReasoningMode`.

| Upstream scenario | Cases | Native assertion |
| --- | ---: | --- |
| Magistral without a map, medium | 1 | Prompt mode reasoning; effort absent |
| Magistral without a map, default off | 1 | Both controls absent |
| Small 2603, Medium latest, GLM-5.2: enabled high | 3 | Effort high; prompt mode absent |
| Same three models: unsupported low | 3 | Effort clamped to high |
| Same three models: default off | 3 | Effort none; prompt mode absent |
| GLM-5.2 max | 1 | Effort max |
| GLM-5.3 low/high/max | 3 | Requested effort; prompt mode absent |
| GLM-5.3 medium | 1 | Effort high |
| Non-reasoning model, medium | 1 | Both controls absent |
| Session ID | 1 | Prompt cache key present |
| Cache retention none | 1 | Prompt cache key absent |

The Go hook receives a typed wire request. Tests project it to JSON values and check snake-case keys. They stop deliberately after payload capture instead of attempting the upstream fixture's refused localhost connection. Request construction and reasoning selection run through the production provider; no reasoning helper is substituted.

### Raw stop reasons: three cases

Source: `packages/ai/test/mistral-raw-stop-reason.test.ts`.
Native: `options_upstream_main_test.go`, `TestUpstreamMainMistralRawStopReasons`.

| Upstream scenario | Native assertion |
| --- | --- |
| stop | Successful stop, raw stop retained, no error message |
| error | Error stop, raw stop retained, exact server-error message, retryable |
| unmapped_error | Error stop, raw stop retained, exact message, not retryable |

Local HTTP servers replace fetch responses. All three cases use the production request and SSE paths.

### HTTP transport: 11 cases

Source: `packages/ai/test/mistral-http-transport.test.ts`.
Native: `inference/provider/mistral/http_transport_upstream_main_test.go`, `TestUpstreamMainMistralHTTPTransport`.

| Upstream scenario | Native coverage and adaptation |
| --- | --- |
| SDK-style payload serialization | Wire fields, nested response schema, user images, headers, payload/response hooks and endpoint suffix checked. **Partial provider-option API coverage:** Go lacks direct promptMode, reasoningEffort and named-function tool-choice fields. The fixture supplies these through the payload hook. Their availability before that hook is not verified. |
| Assistant thinking/tool/result replay | Exact JSON message values, including image result, prefix false and tool index |
| Thinking/text/fragmented tools/cache usage | Exact content and response/raw stop IDs; input 7, output 4, cache read 3, cache write 0, total 14 |
| Empty GLM content deltas | Original nine-delta sequence and exact merged thinking/text/tool content |
| Raw event observation | Original provider metadata and both full event values; model identity and normalised result |
| Bytewise SSE/UTF-8 | Reuses `TestV0842MistralRawByteSplitUTF8Stream`, asserting the same split-byte text and successful stop |
| Header override/suppression | Combined model authorization/affinity overrides, explicit suppression and caller user-agent |
| Abort awaiting a chunk | Cancel after admission, before data; requires aborted and forbids done |
| Header timeout | Exact error result and 5ms diagnostic |
| Active stream beyond timeout | Original five thinking chunks, 10ms spacing, 20ms admission timeout; exact final thinking |
| HTTP error body | Exact 403 status/body diagnostic |

Go's `SuppressHeaders` represents upstream null header overrides. Response header names are compared case-insensitively, and the expected user-agent uses the native platform formatter. Servers replace fetch; one-byte readers replace bytewise ReadableStreams. Local base URLs retain the expected `/v1/chat/completions` suffix. Arbitrary endpoint-resolution differences still need their existing disposition.

### Tool schema: one case

Source: `packages/ai/test/mistral-tool-schema.test.ts`.
Native: `options_upstream_main_test.go`, `TestUpstreamMainMistralToolSchema`.

The production payload hook checks one tool, strict require, and the nested string schema. A deliberate HTTP failure checks error completion without an SDK input-validation failure. **Representation limit:** Go tool parameters are JSON-backed and cannot contain TypeBox symbol properties. The three JavaScript symbol-enumeration assertions are not executed natively; nested JSON assertions do not substitute for them.

### Baseline verification and next tests

The focused upstream ports pass on unchanged production. Full root normal tests, focused Mistral race tests, vet and diff checks also pass with Go 1.27.3 and GOMAXPROCS=4. Harness type/syntax errors and an unread-request-body cleanup hang were corrected before recording this result; none demonstrated a production failure. Temporary upstream copies and disposable logs were removed after analysis.

The upstream cached-usage test covers the plural snake-case alias only. Additional partial-state behaviour and observer projection/mutation differences need separate source-derived regressions before implementation changes. Full convergence and acceptance profiling are incomplete.

### Source-derived usage regressions

`usage_alias_main_test.go` and `testdata/usage_alias_main.json` supplement the upstream ports. Thirteen integral-count cases were checked against the actual pinned `getMistralCachedPromptTokens`: six aliases, precedence, null/missing fallthrough, explicit zero, bounds and absence. Eight failed on unchanged `b96f320` production; five passed. No implementation edits preceded that failure run.

The fix uses nullable integer fields and the pinned six-alias order. All 13 now pass through production HTTP/SSE, including input/cache allocation, total-token fallback and independently calculated estimated cost. Native integer-count validation and existing bounds remain; this does not establish equivalence for fractional or malformed JavaScript number inputs. Full root normal tests, Mistral race and vet pass. Temporary reference copies and disposable logs were removed after analysis.

### Source-derived partial-state regressions

Three cases in `partial_state_main_test.go` and `testdata/partial_state_main.json` fail on unchanged production after their expectations are confirmed by the actual pinned consumeChatStream. Snapshots are copied at synchronous text-delta publication boundaries. Length/error reasons must be visible before EOF; an empty later reason preserves the previous state; a later successful stop preserves an earlier error diagnostic.

Moving finish-reason mapping before delta publication fixes all three failures, including the final retained diagnostic. Production HTTP/SSE tests check snapshots and terminal event counts. Native detached event snapshots, cancellation, DTO/argument authority and reader ownership are unchanged. Full normal tests, Mistral race and vet pass; temporary source helpers and logs were deleted after comparison. Observer projection/mutation, provider-option API and number-domain differences still need dispositions.
