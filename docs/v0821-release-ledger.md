# v0.82.1 release-only parity ledger

Scope: official `@earendil-works/pi-ai` release `v0.82.1` at `b4f293684bba718d59cc1157679bcf6157b3a7f5`, compared only against accepted `v0.82.0` at `083e61621276bff9f6faefab87ce07fcd98734e2`. Do not chase beyond tag.

Exact source checkouts used:

- old: `/workspace/tmp/pi-v0820` → `083e61621276bff9f6faefab87ce07fcd98734e2` (`v0.82.0`)
- new: `/workspace/tmp/pi-v0821` → `b4f293684bba718d59cc1157679bcf6157b3a7f5` (`v0.82.1`)

## Adopted Go-facing changes

- Text model catalog regenerated from exact v0.82.1 data. Comparator evidence: `upstream pairs: 1109`, `generated pairs: 1109`, exact match.
- Image model catalog unchanged in count and regenerated/compared against exact v0.82.1 `src/image-models.generated.ts`. Comparator evidence: `upstream image pairs: 40`, `generated image pairs: 40`, exact match.
- `ModelsError` added to preserve cause in `Error()` and `Unwrap()`.
- `ModelsStoreEntry` now preserves remote catalog `ETag` and `LastModified` metadata; Radius dynamic catalog refresh sends validators, persists updated validators, updates `CheckedAt`, and reuses cached models on `304 Not Modified`.
- Claude Opus 5 / Bedrock catalog support covered by regenerated metadata and Opus 5 tests.
- Radius OAuth now routes device/refresh through gateway endpoints directly, with fallback to legacy discovery for compatibility; local-server routing test added.
- Anthropic bearer-token env auth: `ANTHROPIC_AUTH_TOKEN` is used as an `Authorization: Bearer` header instead of `X-Api-Key`; deterministic provider test added.

## Changed-path disposition matrix

| Status | Path | Disposition |
| --- | --- | --- |
| `M` | `CHANGELOG.md` | Docs/package metadata; recorded, no Go runtime delta. |
| `M` | `package.json` | Docs/package metadata; recorded, no Go runtime delta. |
| `M` | `scripts/generate-models.ts` | Generated model catalog/data handling; regenerated Go catalog from exact v0.82.1 data and updated exact metadata regressions. |
| `M` | `src/api/bedrock-converse-stream.ts` | Claude Opus 5 / Bedrock support settings; covered by regenerated catalog and Opus 5 metadata tests. |
| `M` | `src/auth/oauth/radius.ts` | Radius OAuth direct gateway routing; Go Radius refresh/login prefer gateway endpoints with fallback, local-server tests added. |
| `M` | `src/auth/resolve.ts` | ModelsError cause preservation; Go ModelsError preserves cause in Error()/Unwrap with tests. |
| `M` | `src/env-api-keys.ts` | Anthropic bearer-token env auth; Go Anthropic provider honors ANTHROPIC_AUTH_TOKEN as Authorization bearer header with tests. |
| `M` | `src/models-store.ts` | Models store ETag/Last-Modified revalidation metadata; Go ModelsStoreEntry and revalidation header helpers preserve/use validators with tests. |
| `M` | `src/providers/anthropic.ts` | Anthropic bearer-token env auth; Go Anthropic provider honors ANTHROPIC_AUTH_TOKEN as Authorization bearer header with tests. |
| `M` | `src/utils/error-body.ts` | Provider error body/cause preservation; existing provider-error-body tests plus ModelsError cause coverage apply. |
| `M` | `test/anthropic-adaptive-thinking-models.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live/TS harness portions N/A. |
| `A` | `test/anthropic-auth-token.test.ts` | Anthropic bearer-token env auth; Go Anthropic provider honors ANTHROPIC_AUTH_TOKEN as Authorization bearer header with tests. |
| `M` | `test/bedrock-models.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live/TS harness portions N/A. |
| `M` | `test/bedrock-thinking-payload.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live/TS harness portions N/A. |
| `M` | `test/env-api-keys.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live/TS harness portions N/A. |
| `M` | `test/error-body.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live/TS harness portions N/A. |
| `M` | `test/models-runtime.test.ts` | Models store ETag/Last-Modified revalidation metadata; Go ModelsStoreEntry and revalidation header helpers preserve/use validators with tests. |
| `M` | `test/openai-responses-reasoning-replay-e2e.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live/TS harness portions N/A. |
| `M` | `test/provider-error-body-regression.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live/TS harness portions N/A. |
| `M` | `test/providers.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live/TS harness portions N/A. |
| `A` | `test/radius-oauth.test.ts` | Radius OAuth direct gateway routing; Go Radius refresh/login prefer gateway endpoints with fallback, local-server tests added. |
| `M` | `test/supports-xhigh.test.ts` | Upstream test delta classified; applicable behavior covered by local deterministic Go tests, live/TS harness portions N/A. |
| `M` | `test/xhigh.test.ts` | Claude Opus 5 / Bedrock support settings; covered by regenerated catalog and Opus 5 metadata tests. |

## Validation evidence

Passed before commit/push:

- `PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-v0821-json/providers python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0821/packages/ai/src/providers` → `1109`/`1109` exact match
- image pair comparator against `/workspace/tmp/pi-v0821/packages/ai/src/image-models.generated.ts` → `40`/`40` exact match
- `make check`
- `TMPDIR=/workspace/tmp go test -shuffle=on ./...`
- `TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1`
- `go vet ./...`
- `make staticcheck`
- `make check-logging`
- `make test-repro`
