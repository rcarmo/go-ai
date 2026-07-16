# main `2be9efa` parity ledger

Source audited: authoritative `packages/ai` upstream main `2be9efa19cd64aed40ca63f92c0c0f9a6bac7c9d` compared with previously accepted `dcfe36c79702ec240b146c45f167ab75ecddd205`. npm remains `0.80.7` / `818d67457cdd6b60bce6b121d16b23141c252dd8`, so this is an upstream-main delta, not an npm release.

Mechanical enumeration: `git diff --name-status --no-index` across `packages/ai` found 58 changed package paths plus package docs/metadata. Test inventory is now 101 `packages/ai/test/*.test.ts` files, adding `cloudflare-stream.test.ts`, `providers.test.ts`, `xai-oauth.test.ts`, and `xai-responses.test.ts` relative to the prior accepted baseline.

## Adopted Go-facing changes

| Upstream area | Go status | Evidence |
| --- | --- | --- |
| Generated fallback catalog published through split provider maps/R2 workflow. | **Adopted mechanically.** | Regenerated `models_generated.go` from exact upstream `src/models.generated.ts`; header is `1065 models, 35 providers`. `scripts/compare-upstream-models.py /workspace/tmp/pi-src-2be9efa/pi-2be9efa19cd64aed40ca63f92c0c0f9a6bac7c9d/packages/ai/src/providers` reports `upstream pairs: 1065`, `generated pairs: 1065`, exact match. |
| Model runtime replacing static-only registry: provider-owned model lists, provider-scoped model store, dynamic refresh, cache restore/fallback, concurrency, cancellation. | **Ported as Go-native facade.** | Added `models_runtime.go` with `ModelRuntime`, `ModelsStore`, `InMemoryModelsStore`, provider-scoped `ModelRefreshContext`, concurrent refresh, in-flight deduplication, stored-catalog restore, allowNetwork=false cache-only initialization, and cached-model retention on refresh errors. `models_runtime_test.go` covers restore+persist, error fallback, offline cache-only, concurrent dedupe, and cancellation. Existing global registry remains for compatibility. |
| Dynamic Radius provider catalog behavior moved out of OAuth credential helper. | **Adapted.** | Existing `oauth.RadiusProvider` still discovers `/v1/oauth`, refreshes tokens, loads `/v1/config`, caches gateway config, falls back to prior config on transient failures, and injects `pi-messages` models via `ModifyModels`. The new runtime facade provides the upstream-style provider-scoped store/refresh surface for dynamic providers without breaking the existing Go OAuth package API. |
| xAI OAuth device flow. | **Ported.** | Added `oauth/xai.go`, registered as provider `xai`: device-code request to `auth.x.ai`, `referrer=pi`, HTTPS verification URI validation, wait-before-first-poll device polling, `authorization_pending`, `slow_down` with server interval/default increment, terminal `access_denied`/`authorization_denied`/`expired_token`, refresh token rotation/preservation, default one-hour lifetime, and upstream error detail surfacing. `oauth/xai_test.go` covers these with local servers. |
| xAI Grok 4.5 routed through OpenAI Responses. | **Ported.** | Regenerated catalog sets `xai/grok-4.5` to `api: openai-responses` with xAI base URL and no long cache retention. `inference/provider/openairesponses/xai_responses_upstream_test.go` verifies `grok-4.5`, supported thinking levels, `/responses` request shape, `store=false`, `prompt_cache_key`, reasoning include, and absence of `prompt_cache_retention`. |
| Provider runtime facade/auth dispatch tests (`providers.test.ts`). | **Partially applicable, ported Go-native core.** | Go still exposes package-level APIs and side-effect provider registration rather than TS provider factories/auth stores. Applicable model runtime/store/refresh/cache/concurrency semantics are covered by `models_runtime_test.go`; existing auth/env/provider tests cover Go request auth behavior. |
| Cloudflare stream test. | **Already covered / no new Go gap found.** | Go has Cloudflare provider/env catalog metadata and OpenAI-compatible stream handling through existing OpenAI-compatible provider tests; no distinct wire runtime was introduced in Go for this upstream test beyond existing API-key/env and SSE behavior. |
| JS packaging/lazy entrypoint/export changes. | **N/A.** | TypeScript package entrypoint/lazy import/R2 publication plumbing has no Go package analogue. The generated fallback catalog was compared directly against upstream source maps. |

## Validation evidence

Focused validation passed before full gate:

- `TMPDIR=/workspace/tmp go test . ./oauth ./inference/provider/openairesponses -run 'ModelRuntime|XAI' -count=3`
- `scripts/compare-upstream-models.py /workspace/tmp/pi-src-2be9efa/pi-2be9efa19cd64aed40ca63f92c0c0f9a6bac7c9d/packages/ai/src/providers` → exact 1065/1065 match.

Full gate to run before commit/push:

- `make check`
- `TMPDIR=/workspace/tmp go test -shuffle=on ./...`
- `TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1`
- `go vet ./...`
- `make staticcheck`
- `make check-logging`
- `make test-repro`
