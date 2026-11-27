# OpenAI Completions keeps opaque reasoning for same-model replay

OpenAI Completions stores the latest `reasoning_opaque` in the thinking block's `thinkingSignature`. Opaque-only chunks create a thinking block without a text delta. Replay sends the saved token and, if its signature names a reasoning field, the thinking text. Structured `reasoning_details` still take precedence. Replay uses the existing API, provider and requested-model checks.

## Pinned originals

All seven unchanged `packages/ai/test/openai-completions-reasoning-opaque.test.ts` cases passed at official pi v1.7.0 `fb0fe823539f6d04850f6e34748c01f01f212e1f`, with SHA-256 `bc41a8d2247acf2b691afec5b27ff2839c1f1dc879d0a8e64ba693e03f1b8283`. The [shared Node receipt](ai-originals.json) also records 16 unchanged stored-signature cases, for 59 passing selected originals overall. The opaque test, stored-signature test, adapter, reasoning helper and message transform matched pinned Git bytes. The lane used the verified stage read-only. Vitest 4.1.12 ran under Node 26.13.0 with only loopback and no live service.

## Public Go comparisons

The native fixture uses the pinned `gemini-test` GitHub Copilot model, seven original chunk sequences, an inert HTTP transport, the public `Stream` entry point and a separately captured replay request. Go's absent JSON fields represent JavaScript `undefined`. Both sides check the JSON object stored inside the `thinkingSignature` string using the source field names.

On unchanged Go base `4bbf033e360745c3f41bceaaec84f381d6c880db`, five of seven direct rows failed: an opaque value was missing after text, the most recent value was lost, an opaque-only block was absent, a same-chunk value was lost, and the incomplete stream's partial thinking block kept only a field name. Structured-detail precedence and cross-model isolation already passed. Both stored opaque variants and metadata retention on a provider-event callback refusal failed on the base. Invalid saved values and malformed wire metadata were already ignored.

The fixed code passes all seven direct rows and all 16 original assertion sites. Five stored-value rows check envelopes with and without a reasoning field and rejection of an empty token, an unrecognised field or an explicit null field. Two more native rows cover an early provider callback refusal and a numeric wire token next to valid visible text. These seven adjunct rows do not map the complete 16-case stored-format suite. The existing structured-detail validator and general SSE error-frame handling have not been retargeted; no other provider or live Copilot behaviour was tested.

## Compiled controls

Six compiled controls failed the relevant rows: discard wire opaque values, keep the first instead of the latest, prefer opaque values over structured details, trust invalid stored fields, lose earlier metadata when a callback rejects a provider event, or bypass requested-model isolation. After byte restoration, the focused tests, full OpenAI provider package, vet and Staticcheck 0.8.2 passed with Go 1.27.3 and the shared verification lock:

```sh
unshare -Un go test ./inference/provider/openai \
  -run '^(TestSourceV170CompletionsOpaqueReasoning|TestCompletions(StoredOpaqueReasoning|OpaqueReasoningBoundaries)V170)$' \
  -count=3 -shuffle=42 -p 1 -timeout 45s
unshare -Urn sh -ec 'ip link set lo up; exec "$@"' sh go test \
  ./inference/provider/openai -count=1 -shuffle=42 -p 1 -timeout 75s
unshare -Un go vet -p 1 ./inference/provider/openai
```
