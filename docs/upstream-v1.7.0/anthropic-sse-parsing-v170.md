# v1.7.0 Anthropic public SSE parsing receipt

This bounded packet verifies `packages/ai/test/anthropic-sse-parsing.test.ts` unchanged at official pi v1.7.0 `fb0fe823539f6d04850f6e34748c01f01f212e1f` and compares it with the native Anthropic public stream path. The native base is exact `b1801c447d1193d56e3fadba2ac5100593b76813`; the production/test delta is local commit `a14ab6d851b9692668fb7f24d4575c8d75731b66`. This receipt does not alter that commit.

## Original byte and execution verification

The selected original matched:

- SHA-256: `591ec97b9c5ab0ad494a56624aad56e4ebf42091e7371da54efb070d19a44f6c`
- Git blob: `9f65f831a3e0ec8479fe07a6918496fe351d343d`
- Command: `NODE_OPTIONS=--max-old-space-size=650 ./node_modules/.bin/vitest --run packages/ai/test/anthropic-sse-parsing.test.ts --maxWorkers=1`
- Result: **15 passed twice**, zero skipped, with Vitest 4.1.12 under the shared verification lock. The checked-out test file remained byte-identical. Telemetry/schema were built first and ignored generated provider data was staged from the matching source build solely to satisfy imports; the selected test bytes were not changed.

No live provider was called and no credentials were used. The original injects fake Anthropic SDK responses, so there is no credential-gated assertion to carry as an unmatched native test. Abort, retry scheduling, real Anthropic service admission and SDK constructor identity are outside this original's assertions.

## Mapped native coverage

The crosswalk ledger is [assertions/anthropic-sse-v170.jsonl](assertions/anthropic-sse-v170.jsonl). The two event-observer and response-model cases already mapped in `ai-unchanged-batch2.jsonl` remain authoritative; the new ledger maps the other thirteen cases without duplicating those source sites.

| Original behavior | Native production-path anchor |
| --- | --- |
| Provider event order and model identity | `TestAnthropicProviderStreamEventsForwardInOrder` |
| OpenRouter reported usage cost | `TestAnthropicMainPartialUsageAndBilledCostRawSSE` |
| Requested/serving model and signed thinking replay | `TestAnthropicReturnedModelIsSeparateFromRequestedModel` |
| Returned fallback cost attribution | `TestV0843StreamAnthropicFallbacksHeadersAndPricingEndToEnd` |
| Fallback rejected after output | `TestAnthropicOriginalSSERejectsFallbackAfterOutputBegins` |
| Payload replacement cannot disable streaming | `TestSourceV161AnthropicStrictFallbackResponses` |
| Disabled thinking omits interleaved beta | `TestSourceV160AnthropicInterleavedBetaRequiresThinking` |
| Managed beta propagation | `TestV0850AnthropicManagedEffortHeadersAndResultThinkingLevel` |
| Final serving input transformations | `TestAnthropicOriginalSSEFinalInputTransformations` |
| SSE and tool-argument JSON repair | `TestAnthropicOriginalSSERepairsMalformedToolJSON` |
| Initial block text/thinking/signature | `TestAnthropicContentBlockStartInitialContentAndSignature` |
| Refusal and sensitive stop details | `TestAnthropicOriginalSSEPreservesRefusalAndSensitiveErrors` |
| Missing delta usage and unknown post-stop events | `TestAnthropicMainPartialUsageAndBilledCostRawSSE`, `TestAnthropicOriginalSSEIgnoresUnknownEventsAfterStop` |

## Proven deltas and disposition

The translated controls were run on unchanged native code before the production change. They established four mismatches:

1. `message_start` and `message_delta` input transformations were not retained or emitted as an assistant diagnostic. The final delta value is now authoritative, matching the original.
2. A malformed outer SSE JSON string with a repairable escape/control character failed before the provider-specific stream parser could process its valid tool event. JSON-string repair now precedes native decoding without weakening malformed structural JSON handling.
3. `sensitive` terminated as a normal error result without the upstream `Provider stopped with: sensitive` message; refusal/sensitive terminal results now use error events with the expected final message while preserving raw stop reasons.
4. A fallback block arriving after output was silently ignored; it now produces an error containing `unsupported mid-output model fallback` at the public completion boundary.

The translated controls then passed after the fix. The broader unchanged native Anthropic package passed, confirming the baseline behavior around these additions. Unknown SSE event names are filtered before observer dispatch, matching the original's Anthropic-message allowlist.

## Unmatched process and credential boundaries

Three original cases observe fields on an injected JavaScript Anthropic SDK `client.beta.messages.create` callback. Exact in-memory SDK object/process identity is unmatched in Go because the native provider uses the public `net/http` path rather than that SDK object. The externally observable request and SSE semantics are mapped through native anchors above; these JavaScript-process-only sites are recorded as unmatched in the ledger, not relabelled as native SDK coverage.

Credential-bound gaps are **not applicable to these fifteen originals**: all original execution is fake-client/offline, and the native controls use loopback transport or readers. Live Anthropic authentication, service behavior, abort timing against a real socket, and network retry/cancellation policy are not inferred from this passing packet.

## Verification

With `CGO_ENABLED=0`, `GOMEMLIMIT=650MiB`, and `GOMAXPROCS=2`, the committed runtime/test delta passed:

- `go test ./inference/provider/anthropic -count=2`
- `go vet ./inference/provider/anthropic`
- `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./inference/provider/anthropic`
- `git diff HEAD^ HEAD --check`

This is a local bounded evidence receipt, not full v1.7.0 acceptance, live-provider verification, or release authorization.
