# go-ai Gap Analysis — CLOSED

All gaps from the original analysis have been addressed.

## Source: `@earendil-works/pi-ai` v0.75.2

## Sync history

### v0.75.2 (2026-05-18)

Comparative audit (`@earendil-works/pi-ai v0.75.1` → `v0.75.2`) found:

- Upstream changed only package metadata and `dist/models.generated.*`; provider/type/image/OAuth/header/event parsing surfaces were unchanged.
- Regenerated `models_generated.go` from `v0.75.2` (`938 models / 32 providers`).
- Xiaomi and Xiaomi Token Plan OpenAI-compatible models now carry explicit `compat` metadata: `thinkingFormat: "deepseek"` and `requiresReasoningContentOnAssistantMessages: true`.
- Added generated-metadata parity coverage for the Xiaomi compat fields.

Validation gates passed after regeneration and parity test update.

Post-sync hardening audits also tightened retry/image edge cases, nil/error contracts, generated-registry tests, and global test isolation. No remaining parity gap is tracked for v0.75.2.

### v0.75.1 (2026-05-18)

Comparative audit (`@earendil-works/pi-ai v0.75.0` → `v0.75.1`) found:

- Upstream model registry changed again; regenerated `models_generated.go` from `v0.75.1` (`938 models / 32 providers`).
- `gpt-5.4-fast` was removed upstream from the generated registry.
- Upstream `simple-options` logic kept the 0.75.0 max-token fallback change for large-context models, but `go-ai` does not expose the same simple-options helper path, so there was no public API surface to port there.

Validation gates passed after regeneration and full parity checks.

### v0.75.0 (2026-05-18)

Comparative audit (`@earendil-works/pi-ai v0.74.1` → `v0.75.0`) found:

- Upstream model registry changed materially; regenerated `models_generated.go` from `v0.75.0` (`941 models / 32 providers`).
- `simple-options` changed the default `maxTokens` fallback so large-context models now cap the default output budget at 32k when their advertised max token limit is close to the context window.
- No public provider/type/OAuth/header/event-parsing surface changed beyond model metadata and `simple-options` behavior.

Validation gates passed after regeneration and parity review.

### v0.74.1 (2026-05-16)

Comparative audit (`@earendil-works/pi-ai v0.74.0` → `v0.74.1`) found:

- Provider/type surfaces changed upstream (including new image API modules and refreshed provider/type metadata).
- Regenerated `models_generated.go` from `v0.74.1` (`942 models / 32 providers`).
- Updated tests for renamed/rotated Copilot model IDs in generated metadata.
- Image-specific APIs introduced upstream are now ported under `github.com/rcarmo/go-ai/images`: image model registry, `GenerateImages`, image provider registry, OpenRouter image provider, payload/response hooks, timeout/context, retry/`Retry-After` handling, and usage/output parsing.

Validation gates passed after regeneration, image API implementation, and test updates.

### v0.74.0 (2026-05-07)

**Patch release at new upstream namespace.**

Comparative audit (`@mariozechner/pi-ai v0.73.1` → `@earendil-works/pi-ai v0.74.0`) found:

- **Type/API/provider implementations**: no changes in `dist/types.d.ts`, `dist/index.d.ts`, `dist/env-api-keys.js`, or provider runtime files (`openai-*`, `anthropic`, `amazon-bedrock`, `google`, `mistral`).
- **Model registry**: metadata-only change; model set moved from 971 to 969 entries across 31 providers after upstream catalog updates (notably `inclusionai/ling-2.6-1t` metadata normalization).
- **CLI/readme artifacts**: changed upstream but out of scope for the Go library parity surface.

Deep audit result:

- Regenerated `models_generated.go` from `@earendil-works/pi-ai v0.74.0`.
- Updated sync/audit process and scheduler to track `@earendil-works/pi-ai`.
- No provider code or type-surface deltas required for parity.

### v0.73.0 (2026-05-05)

**Minor release.** Xiaomi provider split, model metadata fixes, Bedrock xhigh thinking, Codex fallback diagnostics/session cleanup.

- **Xiaomi breaking change**: built-in `xiaomi` now points to the API-billing endpoint (`https://api.xiaomimimo.com/anthropic`) and keeps `XIAOMI_API_KEY` for that platform key.
- **New providers**: `xiaomi-token-plan-cn`, `xiaomi-token-plan-ams`, and `xiaomi-token-plan-sgp` with `XIAOMI_TOKEN_PLAN_{CN,AMS,SGP}_API_KEY` environment variables.
- **Model registry**: 956 → 971 models and 28 → 31 providers. Includes Xiaomi Token Plan regional catalogs and Qwen/MiniMax metadata fixes (`opencode-go` Qwen/MiniMax now use OpenAI-compatible API metadata where upstream changed it).
- **Type surface**: assistant messages can now carry `diagnostics`; upstream also added session resource cleanup and diagnostic helpers.
- **Bedrock**: Claude Opus 4.7 `xhigh` now preserves the native `xhigh` effort value instead of mapping to `max`/budgeted high.
- **OpenAI Codex**: WebSocket setup failures before message streaming fall back to SSE and attach a `provider_transport_failure` diagnostic. Failed session IDs stay on SSE fallback for subsequent requests. Cached Codex WebSocket sessions are registered for session cleanup.

Complete comparative audit result:

- Added Xiaomi Token Plan provider constants and environment-key mappings.
- Regenerated `models_generated.go` from upstream v0.73.0 (`971 models / 31 providers`).
- Added assistant-message diagnostics and session-resource cleanup helpers to the Go surface.
- Hardened Codex WebSocket behavior to delay `StartEvent` until the first valid WebSocket event, fall back to SSE on pre-stream transport failures, record fallback/debug stats, attach diagnostics to the fallback assistant message, and expose cleanup through `CleanupSessionResources()`.
- Added Bedrock adaptive-thinking request fields for supported Claude models and preserves native `xhigh` for Opus 4.7.
- Added fake-server/regression tests for Codex fallback diagnostics and Bedrock Opus 4.7 native `xhigh` effort.
- Non-library release-note items (`read` rendering, bash incremental output, fuzzy ranking, terminal session fixes) are Pi runtime/tooling concerns and are not applicable to `go-ai`.

### v0.72.1 (2026-05-02)

**Patch release.** Codex transport defaults + model metadata.

- **OpenAI Codex**: default simple/raw transport changed from `sse` to `auto`.
- **OpenAI Codex**: `auto` transport now uses cached WebSocket continuation behavior when a session ID is present.
- **Model registry**: count unchanged (956 models / 28 providers) with one Qwen metadata update.

Deep audit result:

- Updated Codex default transport to `auto`.
- Enabled cached Codex WebSocket context for `auto` transport, matching upstream.
- Regenerated model registry from v0.72.1.
- No public type or OAuth changes.

### v0.72.0 (2026-05-02)

**Minor release.** Model-level thinking maps + Xiaomi provider.

- **New provider**: `xiaomi` with env key `XIAOMI_API_KEY`.
- **New model metadata**: `thinkingLevelMap` on `Model`, plus `ModelThinkingLevel`/`ThinkingLevelMap` concepts.
- **Reasoning behavior**: upstream replaced hard-coded `supportsXhigh`/reasoning effort maps with per-model thinking-level maps.
- **Providers updated**: OpenAI Completions, OpenAI Responses, OpenAI Codex, Mistral, Google, Vertex, Anthropic, Bedrock and Azure Responses now consult `thinkingLevelMap` where applicable.
- **Model registry**: 951 → 956 models, 27 → 28 providers; added Xiaomi `mimo-v2-flash`.

Deep audit result:

- Added `Model.ThinkingLevelMap`, `ModelThinkingLevel`, `ThinkingOff`, `GetSupportedThinkingLevels`, `ClampThinkingLevel`, and `MapThinkingLevel`.
- Regenerated models including thinking-level maps with null unsupported levels.
- Added Xiaomi provider constant and API-key environment mapping.
- Routed OpenAI Completions, OpenAI Responses, OpenAI Codex, Mistral, Google, and Gemini CLI reasoning through `MapThinkingLevel`.
- Removed stale OpenAI-compatible `ReasoningEffortMap` compat behavior in favor of model-level maps.
- No new OAuth/login changes.

Full comparative audit follow-up:

- Updated the model generator to preserve upstream `headers` and `compat` metadata.
- Added compat fields needed by v0.72.0 (`zaiToolStream`, routing maps) and merge them into provider behavior.
- Added OpenAI-compatible request shaping for `zai`, `qwen`, `qwen-chat-template`, `deepseek`, and OpenRouter thinking formats.
- Added Codex cached WebSocket idle TTL to match upstream's 5-minute session cache.

### v0.71.1 (2026-05-01)

**Patch release.** OpenAI Codex WebSocket cached transport + model metadata.

- **New transport value**: `websocket-cached` added to `Transport`.
- **OpenAI Codex**: cached WebSocket sessions can reuse a session-scoped connection and send deltas with `previous_response_id` when the new request extends the previous context.
- **OpenAI Codex debug helpers**: added Go equivalents for cached WebSocket stats reset/get and session close.
- **Model registry**: 949 → 951 models, including Grok 4.3 variants.

Deep audit result:

- Ported the new Codex transport value.
- Added cached WebSocket session handling, continuation delta construction, and debug stats.
- Added tests proving second cached WebSocket requests reuse the connection and send only delta input plus `previous_response_id`.
- No other provider payload/API changes in v0.71.1.

### v0.71.0 (2026-05-01)

**Minor release.** Provider consolidation + new providers + model tracking.

- **Removed from KnownApi**: `google-gemini-cli` (merged into `google-generative-ai`).
- **Removed from KnownProvider**: `google-gemini-cli`, `google-antigravity` (deprecated upstream).
- **New providers**: `moonshotai`, `moonshotai-cn`, `cloudflare-ai-gateway`.
- **New type field**: `responseModel` on `AssistantMessage` — tracks the model ID reported by the provider if different from the requested model.
- **Anthropic**: stream integrity check (message_start without message_stop → error), Cloudflare AI Gateway routing support.
- **Cloudflare**: AI Gateway URLs (`/compat`, `/openai`, `/anthropic` passthrough), `cf-aig-authorization` header.
- **OpenAI Completions**: `responseModel` tracking, `prompt_cache_hit_tokens` fallback for cached token count, Moonshot/Cloudflare AI Gateway compat.
- **OpenAI Responses**: Cloudflare AI Gateway URL resolution + custom auth header.
- **Google Shared**: removed Antigravity references and Gemini 3 thought signature sentinel.
- **Mistral**: `mistral-medium-3.5` added to models needing special reasoning handling.
- **SupportsXhigh**: added `deepseek-v4-flash`.
- **OAuth**: removed Antigravity and Gemini CLI OAuth exports (deprecated).
- **Model registry**: 909 → 949 models, 26 → 27 providers.

Post-sync audit fixes:

- Populated `responseModel` for OpenAI-compatible streamed chunks.
- Normalized OpenAI-compatible cache usage (`prompt_tokens_details.cached_tokens`, `cache_write_tokens`, `prompt_cache_hit_tokens`).
- Added provider-first compat detection and explicit `Model.CompletionsCompat` merge.
- Resolved Cloudflare base URL placeholders in OpenAI Completions, OpenAI Responses, and Anthropic paths.
- Added Cloudflare AI Gateway `cf-aig-authorization` header handling.
- Added cache key/retention request fields for OpenAI Completions and Responses.
- Added Mistral `reasoning_effort` routing for `mistral-medium-3.5` and related small models.
- Added Anthropic incomplete-stream detection (`message_start` without `message_stop`).

### v0.70.6 (2026-04-29)

**New Cloudflare Workers AI provider + Bedrock model matching improvements.**

- **New provider**: `cloudflare-workers-ai` with env-based URL placeholder substitution (`{CLOUDFLARE_ACCOUNT_ID}`).
  Added provider constant, env key, compat detection, and `ResolveCloudflareBaseURL()` helper.
- **Bedrock**: `getModelMatchCandidates()` now normalizes separators (`.`, `_`, `:`, ` ` → `-`) for
  robust matching of inference profile ARNs. `supportsAdaptiveThinking` simplified to use normalized names.
- **Model registry**: 897 → 909 models, 25 → 26 providers.

### v0.70.5 (2026-04-29)

No-op release — identical to v0.70.4.

### v0.70.4 (2026-04-29)

Metadata-only: model registry 890 → 897.

### v0.70.3 (2026-04-29)

**DeepSeek provider + SDK timeout/retry options.**

- **New provider**: `deepseek` added to `KnownProvider`, env key `DEEPSEEK_API_KEY`.
- **New compat flags**: `RequiresReasoningContentOnAssistantMessages`, `thinkingFormat: "deepseek"`.
- **DeepSeek reasoning**: `thinking: { type: "enabled"/"disabled" }` + `reasoning_effort` in OpenAI Completions.
- **New StreamOptions fields**: `TimeoutMs`, `MaxRetries` (SDK-level passthrough; go-ai maps to HTTP client timeout + RetryConfig).
- **6 new models**: deepseek-v4-flash, deepseek-v4-pro (+ OpenRouter aliases), 2 Bedrock Anthropic models.
- **Model registry**: 876 → 884 models, 24 → 25 providers.
- **simple-options**: passthrough of timeoutMs/maxRetries (SDK-specific; go-ai uses raw HTTP).

### v0.70.0 (2026-04-24)

**Behavioral release.** Provider compat refactoring + model updates.

- **New types**: `AnthropicMessagesCompat` (eager tool streaming, long cache retention),
  `OpenAIResponsesCompat` (session ID header, long cache retention).
  Added `SupportsLongCacheRetention` to `OpenAICompletionsCompat`.
  Model struct now carries `CompletionsCompat`, `ResponsesCompat`, `AnthropicCompat` fields.
- **Anthropic provider**: compat-driven beta headers (`fine-grained-tool-streaming-2025-05-14`,
  `interleaved-thinking-2025-05-14`), compat-driven cache TTL instead of URL-sniffing.
  Ported to go-ai.
- **OpenAI Completions**: content index tracking refactored (use `indexOf` instead of `length-1`).
  Go provider already uses index-based tracking — no change needed.
- **OpenAI Responses**: compat-driven cache retention + session ID header.
  Noted as minor gap — go-ai already skips session headers when not applicable.
- **OpenAI Codex**: gpt-5.5 support in effort mapping + model-specific service tier multiplier.
  go-ai updated `SupportsXhigh` for gpt-5.5. Service tier pricing not yet implemented (existing gap).
- **Google Vertex**: `buildHttpOptions` with `ResourceScope` for custom base URLs.
  Go provider uses raw HTTP — not directly applicable but noted.
- **5 new models**: gpt-5.5, gemma-4-26b-a4b-it, hy3-preview-free, ling-2.6-1t, hy3-preview.
- **2 removed models**: arcee-ai/trinity-large-preview, gemma-4-26b-it.
- **3 pricing changes**: mistral-nemo, qwen3-235b-a22b-thinking, mimo-v2-flash.
- **Model registry**: 871 → 876 models. Regenerated.

### v0.69.0 (2026-04-23)

**Minor release.** Dependency cleanup + model registry update.

- **TypeBox**: `@sinclair/typebox` → `typebox` v1.1.24 (major package rename). No go-ai impact — we use `json.RawMessage`.
- **ajv + ajv-formats removed** upstream. No go-ai impact — we have our own validation.
- **transform-messages**: added `insertSyntheticToolResults()` call at end of transform. go-ai already had this.
- **4 new models**: Xiaomi `mimo-v2.5`, `mimo-v2.5-pro` (+ OpenRouter aliases). Regenerated.
- **1 pricing change**: `gemini-3.1-flash-lite-preview` now free (0/0). Regenerated.
- **Model registry**: 865 → 871 models. Regenerated via `go run scripts/generate-models.go`.
- **No provider behavior changes**, no OAuth changes, no type/event changes.

### v0.68.1 (2026-04-22)

Upstream `v0.68.1` did not introduce a large behavioral delta relative to the already-synced `go-ai` codebase. The practical sync adjustments in this pass were provider-metadata parity updates (`zai`, `huggingface`, `fireworks`) plus continued test/transport hardening.

## Final status

### Core Framework — ✅ Complete
- types, events, registry (with unregister/clear), env, stream, complete
- models.generated (938 models, 32 providers, code generator)
- CalculateCost, SupportsXhigh, ModelsAreEqual
- CLI: skipped (not part of library API)

### Providers — ✅ Complete (10 APIs, 11 with aliases)
| Provider | API | Status |
|---|---|---|
| OpenAI Completions | `openai-completions` | ✅ + full compat flags |
| Anthropic Messages | `anthropic-messages` | ✅ + image support |
| OpenAI Responses | `openai-responses` | ✅ |
| Azure OpenAI | `azure-openai-responses` | ✅ (alias) |
| Google Generative AI | `google-generative-ai` | ✅ |
| Google Vertex AI | `google-vertex` | ✅ (alias) |
| Google Gemini CLI | `google-gemini-cli` | ✅ |
| Mistral | `mistral-conversations` | ✅ |
| Amazon Bedrock | `bedrock-converse-stream` | ✅ |
| OpenAI Codex | `openai-codex-responses` | ✅ (WebSocket + SSE) |
| Faux (test) | `faux` | ✅ |

### Provider Support Modules — ✅ Complete
- simple-options → simple_options.go
- transform-messages → transform.go
- github-copilot-headers → copilot_headers.go
- compat flags → compat.go (16 flags, URL auto-detection)
- google-shared / openai-responses-shared → inlined in providers

### OAuth — ✅ Complete (5 providers)
| Provider | Flow | Status |
|---|---|---|
| GitHub Copilot | Device code | ✅ |
| Anthropic | Authorization code + PKCE | ✅ |
| Google Gemini CLI | Authorization code + PKCE | ✅ |
| OpenAI Codex | Device code | ✅ |
| PKCE utilities | - | ✅ |

### Utilities — ✅ Complete
| Utility | Status |
|---|---|
| transports/sse (SSE parser) | ✅ |
| json-parse (partial JSON) | ✅ |
| overflow (context overflow detection) | ✅ |
| validation (tool call validation) | ✅ |
| hash (short deterministic hash) | ✅ |
| sanitize-unicode | ✅ |
| typebox-helpers | ⏭️ Skip (Go uses json.RawMessage) |
| headers | ⏭️ Skip (trivial in Go) |
| oauth-page | ⏭️ Skip (browser-only HTML) |

### Quality Infrastructure — ✅ Complete
- Centralized pluggable logger (zero-cost default)
- Logging quality gate (scripts/check-logging.sh)
- 5 fuzz targets
- 87+ test functions
- GitHub Actions CI (build, test, coverage, fuzz, logging gate)
- Production Makefile

## Coverage summary

| Category | pi-ai (JS) | go-ai (Go) | Coverage |
|---|---|---|---|
| Core + utils | 723 | 1,800+ | ~100% |
| Providers | 6,887 | 4,900+ | ~100% (all APIs) |
| OAuth | 2,120 | 1,500+ | ~100% (all flows) |
| Models generated | 15,156+ | 11,100+ | 100% (code gen) |
| CLI | 115 | — | Skip |
| **Total** | **24,278** | **18,597+** | **Feature complete** |
