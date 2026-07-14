# Audit Report

Final audit snapshot after the current hardening pass.

## 2026-07-14 v0.80.7 complete comparative audit (`@earendil-works/pi-ai`)

Compared accepted upstream baseline `0e6909f050eeb15e8f6c05185511f3788357ddb3` against published `v0.80.7` / npm `gitHead` `818d67457cdd6b60bce6b121d16b23141c252dd8`; current upstream `main` HEAD was `9d09075c53812f7af955ce4397d0508c4a62efac`. The npm package is `0.80.7`, shasum `6125379d71fe8314c2166e7cddb6e4b847213562`, downloaded SHA-256 `83da6f7122ccc45bfc9d13ebe5db3d6171131c919e3b8cc0cbeefce304704bd1`.

Mechanical diff found 25 `packages/ai` paths changed. Go-facing adoption: regenerated the text provider catalog to exact v0.80.7 package output (`1065 models / 35 providers`), added the exported `ApiPiMessages` constant/alias for custom model metadata round-trip, and updated deterministic catalog regressions for v0.80.7 pricing/window/token changes plus split GPT-5.6 IDs. N/A: the new TS `pi-messages` Radius backend and Radius OAuth helper have no built-in model consumer in v0.80.7 and are outside current Go provider runtime scope; image model catalog did not change.

Validation: `go test ./...`, `go test -shuffle=on ./...`, `go test -race ./...`, `make check`, and `make test-repro` all passed. Full ledger: `docs/v0807-adversarial-ledger.md`.

## 2026-07-13 v0.80.6 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.80.5` (`cc62baa`) against `v0.80.6` (`2b3fda9921b5590f285165287bd442a25817f17b`) and audited `go-ai` parity file-by-file across `packages/ai/src`, generated provider registries, and upstream deterministic tests.

Findings:

- Upstream added the `max` thinking level, expanded provider thinking maps, and changed supported-level reporting so adaptive Anthropic/Copilot/Bedrock/OpenRouter models can expose `max` separately from native `xhigh`.
- Upstream added tiered model-cost metadata and made cost calculation choose the highest matching request-wide input-token threshold.
- Upstream Responses usage now accounts for `input_tokens_details.cache_write_tokens`, subtracting cached and cache-write tokens from billed input while preserving cache-write usage.
- Upstream context estimation now ignores assistant usage snapshots that predate a newer prefix/summary message.
- Upstream refreshed generated text model metadata; exact upstream-main `0e6909f050eeb15e8f6c05185511f3788357ddb3` has 1057 models / 35 providers. Direct provider-map comparison confirms OpenAI and Azure OpenAI Responses `gpt-5.6` are not present and are intentionally absent from Go.
- Fresh adversarial re-check found image model catalog drift: exact v0.80.6 has 35 OpenRouter image models, while Go still had 34 plus stale Sourceful preview entries.
- Upstream README/package/changelog and JS-only lazy/module-load plumbing changes are not applicable to the Go library architecture.

Actions:

- Regenerated `models_generated.go` from upstream `v0.80.6`.
- Regenerated/synced `images/models_generated.go` to exact v0.80.6 image catalog and added regression assertions for count, newly-added image IDs, and removed stale IDs.
- Added Go type/runtime support for `ThinkingMax`, tiered `ModelCost`, Responses cache-write token accounting, and prefix-aware context estimates.
- Updated upstream parity tests for `max`/`xhigh` behavior and generated metadata expectations.
- Added v0.80.6 regression coverage for thinking `max`, cost tiers, and context estimate invalidation.
- Retained port-specific GitHub Copilot end-to-end helper work (side-effect provider package, OAuth runtime bridge, model picker/switch helpers, and example) without dropping existing provider functionality.

Result: `go-ai` is synced with upstream `v0.80.6`; the fresh adversarial pass fixed the concrete image-catalog drift and found no remaining Go-facing runtime/type/provider gaps, with JS-only packaging/auth-collection internals marked not applicable.

## 2026-06-23 v0.80.2 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.80.1` against `v0.80.2` and audited `go-ai` parity.

Findings:

- Upstream restored OpenAI-compatible provider/base-URL compat auto-detection beneath explicit model compat overrides. Go already used `DetectCompatForModel`, so runtime behavior already matched.
- Upstream simplified Anthropic compat defaults to standard Anthropic behavior unless explicit metadata says otherwise. Go already defaulted eager tool streaming and long cache retention on, with no implicit session affinity.
- Upstream adjusted JS auth/model collection internals: `api_key` credential tags, Cloudflare credential `env`, per-request auth overrides, and legacy lazy API aliases. Go uses direct stream/image options, scoped provider env, and explicit provider registration rather than this JS collection layer; no additional wire-protocol change was needed.
- Upstream refreshed two OpenRouter generated metadata entries: `moonshotai/kimi-k2.7-code` and `z-ai/glm-5.2`. No image registry changes were found.

Actions:

- Regenerated `models_generated.go` from upstream v0.80.2.
- Added registry metadata regression coverage for the two changed OpenRouter entries.
- Ran targeted provider/compat/model tests and the full validation gate.

Result: `go-ai` is synced with upstream `v0.80.2`; the complete comparative audit found no remaining Go-facing runtime/type/provider gaps.

## 2026-06-23 v0.80.1 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.79.10` against `v0.80.1` and audited `go-ai` parity.

Findings:

- Upstream reorganized the package around modular `dist/api/` providers, lazy API wrappers, provider factory helpers, and model/auth collection abstractions. Go keeps explicit package registration and global registries, but the generated registry parser needed to support modular provider imports.
- Upstream refreshed generated text model metadata to 999 models / 35 providers. Image model metadata remained at 34 models / 1 provider.
- Upstream added nullable provider headers. A `null` header suppresses provider/API defaults in HTTP-capable providers, while Bedrock filters reserved SigV4/auth headers.
- Upstream OpenAI-compatible and Anthropic-compatible runtime auth now accepts explicit caller auth headers without requiring an `apiKey` value.
- Upstream OpenRouter image generation now carries the richer image options surface: scoped env lookup, abort signal, request timeout, max retries, response hook, and nullable custom headers.
- Upstream OpenAI Codex Responses retries one initial WebSocket connection-limit API error, extracts nested `event.error` codes/messages, and supports nullable extra headers.
- Upstream Bedrock applies custom headers through a Smithy build-step middleware before signing.
- Upstream auth abstractions centralize OAuth refresh. Go's existing `oauth.GetAPIKey()` contract already promised refresh-on-expiry, but the implementation did not perform it.

Actions:

- Updated `scripts/generate-models.go` for the 0.80.x modular registry layout and full compat metadata emission, then regenerated `models_generated.go` from v0.80.1.
- Added `SuppressHeaders` to stream/image options and shared `ApplyHeaders` / `SuppressHeaders` helpers, then wired suppression into HTTP providers and image generation.
- Ported Anthropic-compatible header-owned auth and kept OpenAI-compatible header-owned auth covered.
- Ported OpenRouter image env/timeout/retry/signal/onResponse/header behavior.
- Ported Codex WebSocket connection-limit retry/fallback and nested error extraction.
- Added Bedrock build-step custom-header middleware with reserved-header filtering.
- Fixed OAuth expired-token refresh in the existing Go OAuth helper.
- Added focused regression coverage for each runtime change and representative registry metadata.

Result: `go-ai` is synced with upstream `v0.80.1`; the complete comparative audit found no remaining Go-facing runtime/type/provider gaps. The JS-only `Models`/credential-store collection API remains an architectural difference from Go's existing registry/OAuth packages rather than an unimplemented wire-protocol gap.

## 2026-06-22 v0.79.10 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.79.9` against `v0.79.10` and audited `go-ai` parity.

Findings:

- Upstream changed OpenAI Completions SSE handling for encrypted `reasoning_details`: details are now validated and retained if they arrive before the matching streamed tool-call ID.
- Upstream refreshed generated text model metadata while retaining 979 models / 35 providers.
- Image registry and other provider/type/env/OAuth surfaces were unchanged in `dist/`.

Actions:

- Regenerated `models_generated.go` from upstream v0.79.10.
- Ported pending encrypted-reasoning detail attachment in the OpenAI-compatible SSE parser.
- Added regression coverage for reasoning details preceding tool-call materialization.
- Re-ran the complete validation gate.

Result: `go-ai` is synced with upstream `v0.79.10`; the Go-facing changes were OpenAI-compatible SSE encrypted-reasoning robustness and generated text model registry parity.

## 2026-06-21 v0.79.9 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.79.8` against `v0.79.9` and audited `go-ai` parity.

Findings:

- Upstream added the `chat-template` thinking format for OpenAI-compatible APIs, with configurable `chat_template_kwargs` values and pi-controlled thinking variables.
- Upstream GitHub Copilot OAuth now fetches account-selectable model IDs from `/models` during login/refresh and filters Copilot models when availability data is stored.
- Upstream refreshed `models.generated.*` to 979 models / 35 providers.
- Image registry was unchanged; direct full-`dist/` audit found no other provider/runtime/type/env deltas to port.

Actions:

- Added Go compat/type/generator support for `ChatTemplateKwargValue`, `chatTemplateKwargs`, and `thinkingFormat: "chat-template"` payload construction.
- Updated GitHub Copilot OAuth refresh/login handling and model filtering with legacy-credential fallback.
- Regenerated `models_generated.go` from upstream v0.79.9.
- Added regression coverage for chat-template kwargs and Copilot model availability filtering.
- Re-ran the complete validation gate.

Result: `go-ai` is synced with upstream `v0.79.9`; the Go-facing changes were OpenAI-compatible chat-template thinking kwargs, Copilot OAuth model availability filtering, and generated text model registry parity.

## 2026-06-19 v0.79.8 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.79.7` against `v0.79.8` and audited `go-ai` parity.

Findings:

- Upstream refreshed `models.generated.*` to 981 models / 35 providers.
- Registry deltas are metadata-only: Mistral cache-read prices, OpenRouter cost/window updates, and new `openrouter/fusion`.
- Upstream added side-effect registration modules and `registerApiProvider` calls in JS providers; this is a packaging/registration refactor and does not require a Go runtime change because providers already self-register in `init()`.
- Direct full-`dist/` audit found no type-surface, env-helper, provider behavior, or image registry deltas to port.

Actions:

- Regenerated `models_generated.go` from upstream v0.79.8.
- Added representative registry metadata regression coverage.
- Re-ran the complete validation gate.

Result: `go-ai` is synced with upstream `v0.79.8`; the Go-facing change was generated text model registry parity.

## 2026-06-18 v0.79.7 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.79.6` against `v0.79.7` and audited `go-ai` parity.

Findings:

- Upstream changed only generated registry artifacts: `models.generated.*` and `image-models.generated.*`.
- Text model registry moved to 980 models / 35 providers with metadata additions, removals, and cost/window/max-token updates.
- Image model registry moved to 34 image models / 1 provider, adding Gemini 3 Pro Image and Gemini 3.1 Flash Image OpenRouter entries.
- Direct full-`dist/` audit found no provider/runtime/type/env implementation deltas.

Actions:

- Regenerated `models_generated.go` from upstream v0.79.7.
- Regenerated `images/models_generated.go` from upstream v0.79.7 image registry data.
- Added regression coverage for representative text and image metadata changes.
- Re-ran the complete validation gate.

Result: `go-ai` is synced with upstream `v0.79.7`; the Go-facing change was generated text/image model registry parity.

## 2026-06-17 v0.79.6 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.79.5` against `v0.79.6` and audited `go-ai` parity.

Findings:

- Upstream changed only `models.generated.js`.
- The metadata delta adds `thinkingFormat: "deepseek"` to `opencode-go/deepseek-v4-flash`.
- Direct diff of `dist/types.d.ts`, env helpers, provider runtime files, and image model registry found no other Go-facing changes.

Actions:

- Updated the corresponding `models_generated.go` entry.
- Added registry metadata regression coverage.
- Re-ran the complete validation gate.

Result: `go-ai` is synced with upstream `v0.79.6`; the Go-facing change was a single generated model metadata parity update.

## 2026-06-16 v0.79.5 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.79.4` against `v0.79.5` and audited `go-ai` parity.

Findings:

- Upstream added `StreamOptions.env`, a provider-scoped environment override map used before `process.env` for provider runtime lookups.
- Upstream updated env-key resolution and Cloudflare base URL placeholder substitution to honor scoped env overrides.
- Upstream changed cache-retention default resolution so `PI_CACHE_RETENTION` can come from scoped env for OpenAI Completions, OpenAI Responses, Anthropic, and Bedrock paths.
- Upstream adjusted Bedrock region/GovCloud/force-cache env handling to use scoped env values where applicable; follow-up audit also aligned scoped/explicit AWS region, profile, bearer token, request metadata, skip-auth, endpoint pinning, and HTTP/1 controls.
- Upstream Google Vertex exposes explicit `project`/`location` options and env fallbacks; Go now resolves Vertex REST URLs with the upstream project/location endpoint shape.
- Upstream Azure OpenAI Responses exposes provider-specific API-version/resource/base-URL/deployment options plus env fallbacks and deployment-name map resolution; Go now mirrors those semantics in `StreamOptions` and request construction.
- Upstream OpenAI Codex Responses exposes `textVerbosity` and `reasoningSummary`; Go now forwards both where applicable.
- Upstream added Gemini latest Flash aliases to the Gemini 3 Flash thinking rule.
- Upstream exposes `StreamOptions.websocketConnectTimeoutMs` for OpenAI Codex WebSocket connection setup; Go now carries this field and applies it to Codex WebSocket dialing.
- Upstream refreshed `models.generated.js` to 975 models / 35 providers, adding GLM/Kimi variants and removing stale Gemini aliases; image models remained at 32 models / 1 provider.

Actions:

- Added `ProviderEnv`, `StreamOptions.Env`, `ProviderEnvFromOptions`, scoped env lookup helpers, and cache-retention resolution helpers.
- Added `StreamOptions.WebSocketConnectTimeoutMs` and wired it into OpenAI Codex WebSocket dialing.
- Routed API-key resolution, Cloudflare placeholder substitution, cache-retention defaults, and Bedrock env/auth/endpoint decisions through scoped env/options while preserving normal process-env fallback.
- Added provider-specific option fields for Bedrock, Google Vertex, OpenAI Codex, and Azure OpenAI Responses, then normalized the corresponding request URL/payload construction.
- Updated Google Gemini Flash latest alias matching.
- Regenerated `models_generated.go` from upstream v0.79.5 (975 models / 35 providers) and verified image model parity remained unchanged.
- Re-ran the complete validation gate.

Result: `go-ai` is synced with upstream `v0.79.5`; the Go-facing changes were scoped provider environment support, Bedrock/Azure/Vertex auth/config parity, Codex option/WebSocket timeout parity, registry refresh, and the small cache-retention/Google alias updates.

## 2026-06-15 v0.79.4 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.79.3` against `v0.79.4` and audited `go-ai` parity.

Findings:

- Upstream changed `dist/types.d.ts` by adding optional `Usage.cacheWrite1h`, documenting Anthropic's split for the subset of cache-write tokens written with 1h retention.
- Upstream changed `calculateCost()` so 1h cache writes are charged at `2x` the model input rate; short cache writes continue to use `model.cost.cacheWrite`.
- Upstream changed only Anthropic provider runtime code, parsing `message_start.usage.cache_creation.ephemeral_1h_input_tokens` into `usage.cacheWrite1h`.
- Upstream refreshed `models.generated.js`: 966/968-era Go registry now updates to 971 models / 35 providers for this release, with four model additions and a small set of pricing/max-token/thinking-map/compat changes.
- Direct diff/audit of OpenAI Completions, OpenAI Responses, OpenAI Codex, Google, Mistral, Bedrock, image generation, env API keys, OAuth, provider registration, and index/type surfaces found no other Go-facing runtime deltas.

Actions:

- Added `Usage.CacheWrite1h` to the Go type surface.
- Updated shared `CalculateCost` to match upstream's 1h cache-write accounting.
- Updated Anthropic SSE usage parsing and reused shared cost calculation there.
- Regenerated `models_generated.go` from the v0.79.4 upstream registry snapshot.
- Added regression tests for long-cache-write cost calculation and Anthropic `cacheWrite1h` usage parsing.
- Re-ran the complete validation gate.

Result: `go-ai` is synced with upstream `v0.79.4`; the Go-facing changes were the usage/cost accounting fix, Anthropic parser update, regenerated model registry, and audit/doc updates.

## 2026-06-09 v0.79.0 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.78.1` against `v0.79.0` and audited `go-ai` parity.

Findings:

- Upstream changed OpenRouter request construction so `compat.openRouterRouting` is forwarded whenever present, not only for OpenRouter base URLs.
- Upstream added `compat.supportsDeveloperRole` and now uses `developer` for Responses system prompts only when reasoning is enabled and the provider supports developer-role messages.
- Upstream refreshed the generated model registry to 968 models / 35 providers, adding OpenAI Bedrock, Fireworks, NVIDIA, and OpenRouter entries plus pricing/context metadata updates.
- Direct diff/audit of `dist/types.d.ts`, provider builders, header/base-URL resolution, streaming/event parsing, OAuth/auth behavior, reasoning/thinking maps, and Codex transport/cache handling found no additional Go-facing runtime delta beyond the two compat changes above.
- The Go implementation already matched the new OpenRouter routing and developer-role behavior.

Actions:

- Regenerated `models_generated.go` from the v0.79.0 upstream registry snapshot.
- Verified the Go runtime/provider surface already matched the audited upstream release.
- Re-ran the full validation gates after regeneration.

Result: `go-ai` is synced with upstream `v0.79.0`; the Go-facing changes were the regenerated model registry plus audit/doc updates.

## 2026-05-30 v0.78.0 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.77.0` against `v0.78.0` and audited `go-ai` parity.

Findings:

- Upstream published `v0.78.0` without a model-registry refresh: 931 models / 32 providers remained unchanged.
- The runtime diffs were provider-auth related: OpenAI completions, OpenAI Responses, OpenAI Codex, Anthropic, Google, and Mistral now require explicit API keys in the provider runtime instead of falling back to environment variables.
- Upstream standardized the missing-key error text to `No API key for provider: …` in those paths.
- `dist/types.d.ts` only changed descriptive comments for custom headers and `thinkingFormat`; there was no API-surface, model, header/base-URL, streaming/event, OAuth, reasoning, or Codex transport/cache delta.
- Upstream did not publish separate release notes in the package tarball, so the audit was based on direct code comparison of the published dist artifacts.

Actions:

- Removed environment-key fallback from Go `ResolveAPIKey()` so provider streams require explicit keys, matching upstream 0.78.0.
- Updated provider error strings to match upstream wording for parity and to keep regression output aligned.
- Kept `models_generated.go` unchanged because the upstream registry snapshot did not move.
- Re-ran targeted tests around key resolution and provider error handling, then the full validation suite.

Result: `go-ai` is synced with upstream `v0.78.0`; the Go-facing changes were the explicit-key runtime behavior and matching error text, plus the audit/doc updates.

## 2026-05-29 v0.77.0 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.76.0` against `v0.77.0` and audited `go-ai` parity.

Findings:

- Upstream refreshed the generated model registry: 923 models / 32 providers → 931 models / 32 providers.
- New model metadata covered Claude Opus 4.8 regional variants and reasoning metadata updates, including `thinkingFormat: "string-thinking"` and expanded thinking-level maps.
- Direct diff/audit of `dist/types.d.ts`, provider builders, header/base-URL resolution, streaming/event parsing, OAuth/auth behavior, reasoning/thinking maps, and Codex transport/cache handling found the following parity-relevant wire changes:
  - OpenAI completions gained `string-thinking` support.
  - OpenAI Responses now uses stable fallback IDs for replayed assistant text items.
  - Anthropic-compatible replay preserves empty thinking signatures when requested.
- Upstream did not publish release notes for this patch, so the audit was based on direct package/code comparison.

Actions:

- Regenerated `models_generated.go` from the v0.77.0 upstream registry snapshot.
- Ported the completions and Responses replay updates needed for parity.
- Added regression tests covering the new thinking format and replay behavior.
- Re-ran the full validation gates after the sync.

Result: `go-ai` is synced with upstream `v0.77.0`; the Go-facing changes were the regenerated model registry plus the parity fixes and audit/doc updates.

## 2026-05-28 v0.76.0 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.75.5` against `v0.76.0` and audited `go-ai` parity.

Findings:

- Upstream refreshed the generated model registry: 924 models / 32 providers → 923 models / 32 providers.
- Catalog changes were metadata-only: pricing adjustments across existing models, removal of `cerebras/qwen-3-235b-a22b-instruct-2507`, and additions for `opencode/mimo-v2.5-free` plus `opencode-go/qwen3.7-max`.
- Direct diff/audit of `dist/types.d.ts`, provider builders, header/base-URL resolution, streaming/event parsing, OAuth/auth behavior, reasoning/thinking maps, and Codex transport/cache handling found no Go-facing runtime delta.
- Upstream did not publish release notes for this patch, so the audit was based on direct package/code comparison.

Actions:

- Regenerated `models_generated.go` from the v0.76.0 upstream registry snapshot.
- Verified the Go runtime/provider surface already matched the audited upstream release; no provider, header, streaming, OAuth, or Codex code changes were required.
- Re-ran the full validation gates after regeneration.

Result: `go-ai` is synced with upstream `v0.76.0`; the only Go-facing change was the regenerated model registry and the associated audit/doc updates.

## 2026-05-25 v0.75.5 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.75.4` against `v0.75.5` and audited `go-ai` parity.

Findings:

- Upstream refreshed the generated model registry: 944 models / 32 providers → 924 models / 32 providers.
- New model metadata included Cloudflare Workers AI additions and `compat.forceAdaptiveThinking` updates for Anthropic-compatible built-ins.
- `dist/index.d.ts` re-exported `OAuthDeviceCodeInfo`; the bundled CLI added `onDeviceCode` and `onSelect` OAuth callbacks.
- `package.json` added `@smithy/node-http-handler` as an upstream dependency.

Actions:

- Regenerated `models_generated.go` from the v0.75.5 upstream registry snapshot.
- Confirmed the Go registry now matches the upstream registry data.
- Documented the CLI/OAuth type-surface delta as an intentional divergence because go-ai does not ship the upstream Node CLI layer.
- Re-ran the full validation gates after regeneration.

Result: `go-ai` is synced with upstream `v0.75.5`; the only Go-facing change was the regenerated model registry.

## 2026-05-21 v0.75.4 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.75.3` against `v0.75.4` and audited `go-ai` parity.

Findings:

- Upstream added prompt-cache-key clamping to 64 Unicode code points and wired it into the OpenAI completions, OpenAI Responses, and Codex request builders.
- The JS bundle diff also shows import-extension rewrites in generated output, but there was no runtime protocol or API-surface delta behind them.
- No new model metadata, headers, base URLs, streaming event fields, OAuth flows, or reasoning/thinking maps changed in this release.

Actions:

- Added a Go helper for OpenAI prompt-cache-key clamping and applied it in the OpenAI completions, OpenAI Responses, and Codex request paths.
- Added regression tests covering Unicode-aware truncation to the upstream 64-code-point limit.
- Re-ran the full validation gates after the port.

Result: `go-ai` is synced with upstream `v0.75.4`; the only runtime change was the prompt-cache-key clamp.

## 2026-05-19 v0.75.3 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.75.2` against `v0.75.3` and audited `go-ai` parity after the `images/`, `inference/`, and `transports/` tree split.

Findings:

- Upstream changed only package metadata/version (`package.json`).
- All parity-relevant dist artifacts were byte-identical: text model registry, image model registry, type declarations, provider runtime files, image APIs, OAuth/header/event parsing surfaces, and `simple-options`.

Actions:

- No Go code or generated metadata changes required.
- Re-ran validation gates and tagged the current audited state as `v0.75.3`.

Result: `go-ai` remains behaviorally in sync with upstream `v0.75.3`; this is a no-op sync release.

## 2026-05-18 post-v0.75.2 hardening audit

After syncing `@earendil-works/pi-ai v0.75.2`, repeated code-smell and logic-error audits focused on retry semantics, image generation, generated-registry tests, and global test state.

Actions taken:

- Hardened `Stream`/`Complete` edge cases around nil provider functions and error events.
- Tightened retry behavior and documentation: replayable bodies, `Retry-After`, cancellation, final response ownership, and safe `http.DefaultTransport` handling.
- Hardened OpenRouter image generation retry/cancellation paths, timer cleanup, payload-hook coverage, and registry tests.
- Started breaking package layout split with `images/` for image generation and `inference/` for the text/chat inference facade; text/chat providers now live under `inference/provider/`.
- Started consolidating shared transport primitives under `transports/`, moving SSE parsing to `transports/sse` and adding a WebSocket facade under `transports/websocket`.
- Reduced generated-registry test brittleness by avoiding rotating date-stamped upstream model IDs while retaining compat metadata parity assertions.
- Cleaned unsafe global-state tests and tightened model/provider count thresholds.

Validation: full reproducible gate (`make test-repro`) and race/static/vet/logging checks pass after these follow-up commits.

## 2026-05-18 v0.75.2 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.75.1` against `v0.75.2` and audited `go-ai` parity.

Findings:

- Upstream changed only `models.generated.*` and package metadata.
- No provider/runtime/type/image/OAuth/header/event parsing changes were present.
- The concrete model delta is Xiaomi-family OpenAI-compatible `compat` metadata: DeepSeek thinking format plus required reasoning content on assistant messages.

Actions:

- Regenerated model metadata from upstream `v0.75.2` artifact (`938 models / 32 providers`).
- Added a parity test asserting Xiaomi DeepSeek thinking compat metadata is present.
- Re-ran the full validation suite.

Result: upstream v0.75.2 is synced in Go; no runtime code changes required beyond regenerated metadata and test coverage.

## 2026-05-18 v0.75.1 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.75.0` against `v0.75.1` and audited `go-ai` parity.

Findings:

- Upstream model catalog changed again; regenerated `models_generated.go` now contains `938 models / 32 providers`.
- `gpt-5.4-fast` was removed from the generated registry upstream.
- No provider/type/header/OAuth/event parsing changes were present beyond the generated registry delta and the already-audited 0.75.0 `simple-options` max-token fallback change.

Actions:

- Regenerated model metadata from upstream `v0.75.1` artifact.
- Kept the earlier 0.75.0 simple-options audit note as an intentional non-port: `go-ai` does not expose the same simple-options helper path, so there was no public API surface to change there.
- Re-ran the full validation suite after regeneration.

Result: upstream v0.75.1 is synced in Go, with the only concrete code change being regenerated model metadata.

## 2026-05-18 v0.75.0 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.74.1` against `v0.75.0` and audited `go-ai` parity.

Findings:

- Upstream model catalog changed materially; regenerated `models_generated.go` from `v0.75.0` would contain `941 models / 32 providers`.
- `simple-options` changed the default `maxTokens` fallback so large-context models now cap the default output budget at 32k when their advertised max token limit is close to the context window.
- No public provider/type/OAuth/header/event-parsing surface changed beyond model metadata and `simple-options` behavior.

Actions:

- Audited the new default max-token behavior against the Go codebase and found no direct public helper/API path to port; this remains a release-note-only divergence in `go-ai`’s current surface.
- Regenerated and compared model metadata for parity review.
- Re-ran the full validation gates after the audit.

Result: v0.75.0’s registry changes are represented by regenerated metadata; the simple-options fallback change is recorded as an intentional non-port because `go-ai` does not expose that helper path.

## 2026-05-16 v0.74.1 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@earendil-works/pi-ai v0.74.0` against `v0.74.1` and audited `go-ai` parity.

Findings:

- Upstream introduced broader dist changes (types/index/provider bundles), including new image-related API modules.
- Model catalog changed materially; regenerated `models_generated.go` now contains `942 models / 32 providers`.
- Existing `go-ai` runtime/provider code remained valid for current text/chat parity surface.

Actions:

- Regenerated model metadata from upstream `v0.74.1` artifact.
- Added generated image model registry from upstream `image-models.generated.js` (28 image models / 1 provider), now under `images/`.
- Ported image API surface under `github.com/rcarmo/go-ai/images`: `ImagesApi`, `ImagesModel`, `ImagesContext`, `AssistantImages`, image registry, `GenerateImages`.
- Ported OpenRouter image provider behavior under `github.com/rcarmo/go-ai/images/openrouter`: OpenAI-compatible chat completions payload, `modalities`, data-URL image output parsing, usage/cost parsing, payload/response hooks, timeout/context, retry handling, and provider/header API key handling.
- Updated generated-metadata parity test to current Copilot model IDs and added image API tests.
- Ran verification gates (`make test-repro`).

Result: new upstream image API surface is represented in Go with OpenRouter image provider parity.

## 2026-05-07 v0.74.0 complete comparative audit (`@earendil-works/pi-ai`)

Compared `@mariozechner/pi-ai v0.73.1` against `@earendil-works/pi-ai v0.74.0` and then verified `go-ai` parity.

Findings:

- No changes in provider runtime JS files (`openai-completions`, `openai-responses`, `openai-codex-responses`, `anthropic`, `amazon-bedrock`, `google`, `mistral`).
- No changes in `types.d.ts`, `index.d.ts`, or API key mapping runtime.
- Upstream deltas were model-catalog and package/readme/cli metadata changes.

Actions taken:

- Regenerated `models_generated.go` from `@earendil-works/pi-ai v0.74.0` (969 models / 31 providers).
- Updated process docs and automation to use the new upstream package name.
- Re-ran full validation gates including race/staticcheck/logging checks.

Result: no new logic gaps introduced by the upstream namespace move; parity remains intact after model regeneration.

## 2026-05-05 v0.73.0 complete comparative audit

Compared upstream `@mariozechner/pi-ai` v0.73.0 against the previous v0.72.1 artifact and the current Go implementation. The release was behavioral/API-affecting for the core library.

Closed in this pass:

- **Xiaomi provider split**: `ProviderXiaomi` now tracks the API-billing endpoint through regenerated model metadata, and the three Token Plan regional providers (`xiaomi-token-plan-cn`, `xiaomi-token-plan-ams`, `xiaomi-token-plan-sgp`) have provider constants and environment key mappings.
- **Model registry parity**: regenerated from upstream v0.73.0, now `971 models / 31 providers`, including Qwen/MiniMax OpenAI-compatible metadata fixes and Xiaomi regional catalogs.
- **Assistant diagnostics**: assistant messages now carry optional diagnostics, matching upstream's new transport-diagnostic channel.
- **Session resource cleanup**: added a small cleanup registry and wired OpenAI Codex cached WebSocket sessions into it, so callers can close session-scoped resources via `CleanupSessionResources(sessionID)`.
- **OpenAI Codex WebSocket fallback**: WebSocket `StartEvent` is delayed until the first valid event; setup/read failures before that point fall back to SSE, attach a `provider_transport_failure` diagnostic, and mark the session for future SSE fallback. Debug stats now include WebSocket failure/fallback counters.
- **Bedrock Opus 4.7 xhigh**: Bedrock adaptive-thinking request fields now preserve native `xhigh` effort for Claude Opus 4.7 instead of collapsing it to high/max.

Audited as not applicable to `go-ai`:

- Pi runtime/tool UX changes: incremental bash output streaming, compact read rendering, selector/autocomplete ranking, terminal-input session shutdown, and `/login` display labels.
- Pi CLI/process session shutdown semantics beyond the exported library cleanup hook.

## 2026-05-01 upstream structure/payload audit

Compared `go-ai` against `@mariozechner/pi-ai` v0.70.3 through v0.72.0, with emphasis on model payloads and API metadata. The audit found and fixed the main high-risk gaps introduced by the recent upstream releases:

- **OpenAI-compatible response metadata**: `responseModel` is now parsed from streamed chat-completion chunks when providers report a model ID that differs from the requested model. Response IDs are captured from chunk IDs.
- **OpenAI-compatible usage normalization**: cached-token accounting now supports both `prompt_tokens_details.cached_tokens` and provider-specific `prompt_cache_hit_tokens`, and separates cache reads from cache writes.
- **Cloudflare AI Gateway / Workers AI**: OpenAI Completions, OpenAI Responses, and Anthropic providers now resolve `{CLOUDFLARE_*}` base-URL placeholders at request time. Cloudflare AI Gateway uses `cf-aig-authorization` instead of a normal provider `Authorization`/`X-Api-Key` header.
- **Provider-first OpenAI compat detection**: compatibility inference now mirrors pi-ai's recent provider-first approach for Moonshot, Cloudflare Workers AI, Cloudflare AI Gateway, DeepSeek, OpenRouter, Ollama, etc., and merges explicit `Model.CompletionsCompat` overrides.
- **Cache retention payload fields**: OpenAI Completions and Responses now emit prompt-cache key/retention fields when the caller opts into cache retention and the provider supports the long-retention variant.
- **Mistral reasoning payloads**: `mistral-small-2603`, `mistral-small-latest`, and `mistral-medium-3.5` now use `reasoning_effort` instead of generic `prompt_mode=reasoning`.
- **Anthropic stream integrity**: Anthropic streams that start a message but end without `message_stop` now surface an `ErrorEvent` instead of silently completing.
- **OpenAI Codex cached WebSocket transport**: `websocket-cached` and `auto` with a session ID reuse session-scoped Codex WebSockets and send continuation deltas with `previous_response_id`, with exported debug/session helpers.
- **Model-level thinking maps**: v0.72.0's `thinkingLevelMap` metadata is preserved in generated models and used to clamp/map reasoning levels across OpenAI, Codex, Mistral, Google/Gemini, and Responses providers.

## 2026-05-02 full comparative audit follow-up

A second full audit after the v0.72.0 sync found two remaining parity-sensitive gaps and closed them:

- **Generated model metadata** now preserves upstream `headers` and `compat` objects. This restores model-specific behavior for Copilot headers, DeepSeek/ZAI thinking formats, session-affinity flags, Anthropic eager tool streaming overrides, strict-tool support, and max-token-field overrides.
- **OpenAI-compatible thinking formats** now emit provider-specific controls (`thinking`, nested `reasoning`, `enable_thinking`, `chat_template_kwargs`, and `tool_stream`) based on merged model compat + `thinkingLevelMap`.
- **OpenAI Codex cached WebSocket sessions** now have a 5-minute idle TTL matching upstream's temporary session cache rather than persisting until explicit close.

Intentional divergence retained:

- `google-gemini-cli` and `google-antigravity` constants/providers remain in Go for backward compatibility, although v0.71.0 removed them from upstream public unions.
- Live-provider authentication and exact SDK-only behavior remain unverified by CI; tests cover request construction, SSE parsing, and header routing.

## What is in good shape

- Unified provider API (`Stream` / `Complete`) works across implemented providers.
- HTTP providers honor request hooks and opt-in retry configuration.
- SSE transport failures now surface as `ErrorEvent` instead of silently ending.
- OpenAI Codex supports:
  - WebSocket transport
  - cached WebSocket transport (`websocket-cached`)
  - SSE fallback
  - retryable WebSocket dial setup
- Azure OpenAI Responses has explicit handling for:
  - session headers
  - tool-call history trimming
  - reasoning/commentary normalization
- Examples build and have smoke-tested credential preflight behavior.
- Provider-level fake-server tests now cover the major HTTP/SSE providers plus Codex WebSocket protocol flow.

## Residual architectural gaps

### 1. No built-in stream resume abstraction

Library users can now reliably detect transport failure via `ErrorEvent`, but recovery remains harness-owned.

Still not implemented:
- replay/resume cursor abstraction
- provider-agnostic event checkpointing
- automatic mid-stream resume after disconnect

Recommended pattern today:
- persist context/checkpoints in the harness
- reconnect in an outer loop
- decide whether to retry the same provider/transport or switch

### 2. Bedrock retry behavior is not unified with raw HTTP providers

Bedrock uses the AWS SDK streaming stack rather than the raw `DoWithRetry` HTTP path.

That means:
- `StreamOptions.RetryConfig` is not the single source of truth for Bedrock transport retries
- retry semantics are partly owned by the AWS SDK

What is covered:
- request construction
- stream error surfacing
- stop-reason mapping

What is still missing:
- a unified Bedrock retry contract equivalent to the raw HTTP providers

### 3. Live provider success is not validated in CI

The suite now covers buildability, protocol parsing, request wiring, retry paths, and example preflights.

Still intentionally not implemented:
- CI jobs that hit live provider APIs with real credentials

Reason:
- secret management
- nondeterminism
- network dependency
- cost

### 4. Context compaction is still intentionally basic

`CompactContext()` is tail truncation only.

Still not implemented:
- summarizing compactor
- semantic pruning
- guaranteed preservation of tool-call/result structure across truncation boundaries

### 5. OAuth/login flows remain lightly tested

The OAuth packages compile and expose the intended provider contracts, but browser/device-flow behavior is not deeply integration-tested.

## Dead-code / leftover audit result

Removed during this pass:
- unused transform placeholder state
- unused Google stream parser leftovers

Remaining low-coverage areas are mostly expected:
- example `main()` functions
- OAuth flows that require browser/device interaction
- logger methods exercised indirectly
- live-provider-only branches

## Recommended next work

If continuing hardening, the best next steps are:

1. Add a resumable harness helper around `ErrorEvent` + persisted context.
2. Define a clearer Bedrock retry story relative to `RetryConfig`.
3. Add optional secret-backed live smoke tests outside default CI.
4. Implement a smarter compactor for long-running agents.
