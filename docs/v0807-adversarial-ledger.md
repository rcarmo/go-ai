# v0.80.7 adversarial parity ledger

Source audited: `@earendil-works/pi-ai` package `0.80.7`, npm `gitHead`/tag `818d67457cdd6b60bce6b121d16b23141c252dd8`; current relevant upstream `main` HEAD `9d09075c53812f7af955ce4397d0508c4a62efac`.

Package evidence:

- Registry latest: `0.80.7`.
- Tarball: `https://registry.npmjs.org/@earendil-works/pi-ai/-/pi-ai-0.80.7.tgz`.
- npm shasum: `6125379d71fe8314c2166e7cddb6e4b847213562`.
- npm integrity: `sha512-8RLKLwe5TFM9kKFMNu/lTzveduq4GxZbnlG6ba8FAhLeb5wJP4zbj1eBumKBRvggpFQnW5R/Vo2a8zTlHsV9SQ==`.
- Downloaded tarball SHA-256: `83da6f7122ccc45bfc9d13ebe5db3d6171131c919e3b8cc0cbeefce304704bd1`.

Baseline for reopened audit: accepted upstream `0e6909f050eeb15e8f6c05185511f3788357ddb3`.

Mechanical delta enumeration from `0e6909f` to `v0.80.7` under `packages/ai`: **25 changed paths** — 22 modified/added source-package files, 3 test-file changes including one rename. Main source deltas are generated provider metadata, new `api/pi-messages` TypeScript backend, `pi-messages` export/compat registration, `RADIUS_API_KEY`, and OAuth Radius helpers.

## Discrepancy ledger

| Upstream delta | Go status | Evidence / rationale |
| --- | --- | --- |
| Text provider catalog metadata changed in Amazon Bedrock, Azure/OpenAI, Cloudflare AI Gateway, Copilot, OpenCode, OpenRouter, Vercel, Cerebras. | **Adopted.** | Regenerated `models_generated.go` from exact `0.80.7` package dist. New header: `1065 models, 35 providers`. Regression updates assert exact count, changed Kimi K2.7 Code pricing, GLM-5.2 pricing/window/tokens, and split OpenAI/Azure `gpt-5.6-{luna,sol,terra}` IDs while keeping umbrella `gpt-5.6` absent. |
| `api: "pi-messages"` added and exported in TS. | **Type-level adopted; runtime provider N/A.** | Added exported `ApiPiMessages` constant and inference alias so custom model metadata can round-trip. No generated `0.80.7` built-in model currently uses `pi-messages`; implementing Radius/pi-message streaming is not required for existing Go provider parity and would be a new provider feature. |
| `src/api/pi-messages.ts` and `test/pi-messages.test.ts`. | **N/A with explicit scope.** | TS-only new backend for Radius/custom providers. No Go built-in catalog entry exercises it. Covered by API enum preservation; full stream implementation remains non-applicable until a Go consumer/model needs the backend. |
| OAuth Radius helper (`utils/oauth/radius.ts`) and `RADIUS_API_KEY`. | **N/A.** | Radius gateway credential helper for the new TS backend; Go has no Radius provider/runtime. No existing Go OAuth flow is affected. |
| Image provider/model catalog. | **No change.** | v0.80.7 source/package did not change `image-models.generated`; existing 35 OpenRouter image-model regression remains valid. |
| Upstream tests: renamed OpenAI Responses compat test, added `pi-messages.test.ts`, minor Anthropic e2e prompt edits. | **No Go runtime change except catalog regressions.** | Rename/prompt edits do not change deterministic Go behavior. New pi-messages tests are scoped to non-implemented TS backend, documented N/A. |

## Validation evidence

- `go test ./...` — pass.
- `go test -shuffle=on ./...` — pass.
- `go test -race ./...` — pass.
- `make check` — pass (`go test -count=3`, `go vet`, pinned `staticcheck@v0.7.0`, logging gate).
- `make test-repro` — pass (toolchain info, test/vet/build/staticcheck/logging, race).

No headline skips were added.
