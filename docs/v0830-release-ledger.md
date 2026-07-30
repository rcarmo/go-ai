# v0.83.0 release-only parity ledger

Scope: official `@earendil-works/pi-ai` release `v0.83.0` at `845d6ff1f6643aba440341cce877ce1c43ebbc39`, compared only against accepted `v0.82.1` at `b4f293684bba718d59cc1157679bcf6157b3a7f5`. Do not chase beyond tag.

Exact source checkouts used:

- old: `/workspace/tmp/pi-v0821` → `b4f293684bba718d59cc1157679bcf6157b3a7f5` (`v0.82.1`)
- new: `/workspace/tmp/pi-v0830` → `845d6ff1f6643aba440341cce877ce1c43ebbc39` (`v0.83.0`)

## Adopted Go-facing changes

- Text model catalog regenerated from exact v0.83.0 data. Comparator evidence: `upstream pairs: 1153`, `generated pairs: 1153`, exact match.
- Image model catalog unchanged in count and compared against exact v0.83.0 `src/image-models.generated.ts`: `40`/`40` exact match.
- `Message.RawStopReason` added and populated for OpenAI Completions, Mistral, Google, Bedrock, Anthropic, and Responses/Azure/Codex stream stops; provider error stop messages now preserve raw stop reasons in deterministic tests. Anthropic/Responses/Codex pending streams now error instead of defaulting to success when no terminal stop/status is seen.
- OAuth minimum-validity refresh now refreshes tokens within the default five-minute window and exposes `GetAPIKeyWithMinValidity` for stricter callers; tests cover default refresh and explicit too-short refresh rejection.
- Malformed OpenAI streaming tool deltas carrying valid `function` plus empty `custom` preserve function arguments; regression test added.
- Bedrock credential priority adjusted so explicit/scoped profiles are not silently overridden by ambient AWS access keys; ambient profile remains compatible with ambient keys.
- Other API/test deltas were catalog metadata, TS fetch-option plumbing, or live SDK harness changes with no direct Go analogue beyond existing HTTP client/retry hooks.

## Changed-path disposition matrix

| Status | Path | Disposition |
| --- | --- | --- |
| `M` | `CHANGELOG.md` | Docs/package metadata; recorded, no Go runtime delta. |
| `M` | `README.md` | Docs/package metadata; recorded, no Go runtime delta. |
| `M` | `package.json` | Docs/package metadata; recorded, no Go runtime delta. |
| `M` | `scripts/generate-models.ts` | Generated model catalog/metadata; regenerated Go catalog from exact v0.83.0 data and updated metadata regressions. |
| `M` | `src/api/anthropic-messages.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/azure-openai-responses.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/bedrock-converse-stream.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/google-generative-ai.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/google-vertex.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/mistral-conversations.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/openai-codex-responses.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/openai-completions.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/openai-responses-shared.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/openai-responses.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/openrouter-images.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/pi-messages.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/api/simple-options.ts` | Provider runtime API delta; applicable raw-stop/fetch behavior ported where Go has matching HTTP/SSE surfaces; SDK-only TS fetch plumbing N/A. |
| `M` | `src/auth/oauth/openrouter.ts` | Audited; no direct Go analogue beyond documented catalog/runtime changes. |
| `M` | `src/auth/resolve.ts` | Audited; no direct Go analogue beyond documented catalog/runtime changes. |
| `M` | `src/providers/faux.ts` | Audited; no direct Go analogue beyond documented catalog/runtime changes. |
| `M` | `src/types.ts` | Type surface update; Go Message gains RawStopReason for upstream stop-reason preservation. |
| `M` | `test/anthropic-sse-parsing.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `M` | `test/azure-openai-responses-reasoning-replay.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `A` | `test/bedrock-credentials.test.ts` | Bedrock credential priority; Go Bedrock now avoids ambient access-key override when explicit/scoped profile is configured. |
| `A` | `test/bedrock-raw-stop-reason.test.ts` | Raw provider stop reason preservation; Go Message.RawStopReason and provider tests added for OpenAI, Mistral, Google, and Bedrock. |
| `M` | `test/constrained-sampling.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `M` | `test/faux-provider.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `A` | `test/fetch-option.test.ts` | Custom fetch option; Go uses explicit http.Client/transport hooks rather than JS fetch, N/A except existing HTTP client/retry hooks. |
| `M` | `test/github-copilot-anthropic.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `A` | `test/google-raw-stop-reason.test.ts` | Raw provider stop reason preservation; Go Message.RawStopReason and provider tests added for OpenAI, Mistral, Google, and Bedrock. |
| `A` | `test/mistral-raw-stop-reason.test.ts` | Raw provider stop reason preservation; Go Message.RawStopReason and provider tests added for OpenAI, Mistral, Google, and Bedrock. |
| `M` | `test/models-runtime.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `M` | `test/oauth-auth.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `A` | `test/openai-completions-raw-stop-reason.test.ts` | Raw provider stop reason preservation; Go Message.RawStopReason and provider tests added for OpenAI, Mistral, Google, and Bedrock. |
| `M` | `test/openai-completions-tool-choice.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `M` | `test/openai-responses-partial-json-cleanup.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `M` | `test/openai-responses-terminal-event.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `M` | `test/openrouter-oauth.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `M` | `test/pi-messages.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `M` | `test/qwen-token-plan-models.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |
| `M` | `test/validation.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests or marked TS/live harness N/A. |

## Validation evidence

Passed before commit/push:

- `PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-v0830-json/providers python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0830/packages/ai/src/providers` → `1153`/`1153` exact match
- image pair comparator against `/workspace/tmp/pi-v0830/packages/ai/src/image-models.generated.ts` → `40`/`40` exact match
- `make check`
- `TMPDIR=/workspace/tmp go test -shuffle=on ./...`
- `TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1`
- `go vet ./...`
- `make staticcheck`
- `make check-logging`
- `make test-repro`
