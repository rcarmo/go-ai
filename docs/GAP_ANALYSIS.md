# go-ai Gap Analysis — CLOSED

All gaps from the original analysis have been addressed.

## Source: `@earendil-works/pi-ai` v0.79.5

## Sync history

### v0.79.5 (2026-06-16)

Comparative audit (`@earendil-works/pi-ai v0.79.4` → `v0.79.5`) found:

- **Scoped provider env**: upstream added `StreamOptions.env` and routes provider runtime env lookups through it before process env.
- **API keys and Cloudflare placeholders**: env key resolution and Cloudflare `{VAR}` base URL expansion now honor scoped env overrides.
- **Cache retention**: `PI_CACHE_RETENTION` can be provided via scoped env for OpenAI Completions, OpenAI Responses, Anthropic, and Bedrock defaults.
- **Bedrock**: AWS region/GovCloud detection, credential/profile/bearer-token selection, skip-auth, HTTP/1 opt-in, explicit endpoint pinning, request metadata, and force-cache override now use scoped env/options where relevant.
- **Google Vertex**: explicit `project`/`location` options and env fallbacks now resolve Vertex REST URLs using the upstream project/location endpoint shape.
- **Azure OpenAI Responses**: upstream-specific `azureApiVersion`, `azureResourceName`, `azureBaseUrl`, `azureDeploymentName`, and `AZURE_OPENAI_DEPLOYMENT_NAME_MAP` semantics are reflected in `StreamOptions` and request URL/model construction.
- **OpenAI Codex Responses**: `textVerbosity` and `reasoningSummary` options are now forwarded into the request payload.
- **Google**: `gemini-flash-latest` and `gemini-flash-lite-latest` are included in the Gemini 3 Flash thinking behavior.
- **Codex WebSocket timeout**: upstream exposes `websocketConnectTimeoutMs` for WebSocket connection setup; Go now exposes `StreamOptions.WebSocketConnectTimeoutMs` and applies it to Codex WebSocket dialing.
- **Model registry**: refreshed to 975 models / 35 providers. Added GLM/Kimi variants including `@cf/zai-org/glm-5.2`, `z-ai/glm-5.2`, `zai/glm-5.2`, and high-speed Kimi K2.7 Code entries; removed stale Gemini aliases. Image models remained at 32 models / 1 provider.

Actions:

- Added `ProviderEnv`, `StreamOptions.Env`, and scoped env/cache-retention helper functions.
- Added `StreamOptions.WebSocketConnectTimeoutMs` and mapped it to OpenAI Codex WebSocket connection timeout behavior.
- Added provider-specific option fields for Bedrock region/profile/bearer token/request metadata, Google Vertex project/location, Codex text verbosity, and Azure OpenAI Responses config.
- Added Azure OpenAI Responses option fields and env-aware base URL/deployment resolution.
- Updated provider API-key resolution, Cloudflare base URL substitution, cache-retention defaults, Bedrock env/auth/endpoint handling, Vertex URL construction, Codex request construction, and Google Flash latest alias matching.
- Regenerated `models_generated.go` from v0.79.5 (975 models / 35 providers) and verified `images/models_generated.go` already matched upstream.
- Re-ran the complete validation suite.

Result: upstream v0.79.5 is fully synced in Go; this was a scoped-env/runtime, Bedrock/Azure/Vertex auth/config, Codex option/timeout, and registry parity update.

### v0.79.4 (2026-06-15)

Comparative audit (`@earendil-works/pi-ai v0.79.3` → `v0.79.4`) found:

- **Type surface**: `Usage` gained optional `cacheWrite1h`, the Anthropic-reported subset of `cacheWrite` written with 1h retention.
- **Cost calculation**: upstream now prices 1h cache writes as `2x` base input cost, while normal cache writes continue to use `model.cost.cacheWrite`.
- **Anthropic provider**: `message_start.usage.cache_creation.ephemeral_1h_input_tokens` is parsed into `usage.cacheWrite1h`.
- **Model registry**: refreshed to 971 models / 35 providers. Added `gemma-4-E2B-it`, `gemma-4-E4B-it`, `glm-5.2`, and `moonshotai/Kimi-K2.7-Code`; updated Claude thinking maps, MiniMax compat, and several Kimi/DeepSeek price/max-token fields.
- **No other provider/runtime deltas**: direct diff of OpenAI Completions, OpenAI Responses, OpenAI Codex, Google, Mistral, Bedrock, image provider, OAuth, env-key, registry, and index surfaces found no further Go-facing changes.

Actions:

- Added `Usage.CacheWrite1h` and updated `CalculateCost` to match upstream 1h cache-write pricing.
- Updated Anthropic SSE usage parsing and cost computation.
- Regenerated `models_generated.go` from v0.79.4 (971 models / 35 providers).
- Added regression coverage for Anthropic `cacheWrite1h` parsing and long-cache-write cost calculation.
- Re-ran the complete validation suite.

Result: upstream v0.79.4 is fully synced in Go; this was a small type/model/Anthropic usage accounting update.

### v0.79.0 (2026-06-09)

Comparative audit (`@earendil-works/pi-ai v0.78.1` → `v0.79.0`) found:

- **OpenRouter routing**: upstream now always forwards `compat.openRouterRouting` as the request `provider` field when present, instead of requiring an OpenRouter base URL check.
- **Responses developer role**: upstream added `compat.supportsDeveloperRole` and now uses `developer` only when both reasoning is enabled and the provider supports it; otherwise it falls back to `system`.
- **Model registry**: refreshed to 968 models / 35 providers with new OpenAI Bedrock, Fireworks, NVIDIA, and OpenRouter entries plus pricing/context updates.
- **Go parity**: `go-ai` already matched the new compat behavior, so the sync only needed a regenerated model registry and audit confirmation.

Actions:

- Regenerated `models_generated.go` from v0.79.0 (968 models / 35 providers).
- Re-ran audit checks against OpenRouter routing and OpenAI Responses developer-role selection.
- Re-ran full validation suite.

Result: upstream v0.79.0 is fully synced in Go; this was a metadata-and-compat parity refresh with no additional provider code changes required.

## Source: `@earendil-works/pi-ai` v0.78.1

## Sync history

### v0.78.1 (2026-06-04)

Comparative audit (`@earendil-works/pi-ai v0.78.0` → `v0.78.1`) found:

- **New providers**: `ant-ling`, `nvidia`, `zai-coding-cn` added with env keys and compat autodetection.
- **New compat field**: `supportsTemperature` (defaults true; Claude Opus 4.7+ and some Cloudflare AI Gateway models reject non-default temps).
- **New thinking format**: `"ant-ling"` for the Ant-Ling provider.
- **OpenRouter developer-role restriction**: upstream now only enables `SupportsDeveloperRole` for `anthropic/` and `openai/` prefixed model IDs on OpenRouter.
- **Model registry**: 931 → 968 models / 32 → 35 providers. Ant-Ling, Nvidia models added; Anthropic pricing updated; GitHub Copilot model renames.
- **Image model registry**: 28 → 30 image models (new OpenRouter image models).
- **Bedrock**: empty-text placeholder and tool-result content helpers added upstream (Go bedrock provider already handles empty content).
- **Anthropic provider**: temperature now gated by `compat.supportsTemperature` upstream.
- **OAuth**: GitHub Copilot verification_uri validation; Codex callback host safety (Go OAuth already validates URLs).

Actions:

- Added `ProviderAntLing`, `ProviderNvidia`, `ProviderZAICodingCN` constants and env key mappings.
- Added `SupportsTemperature` field to `OpenAICompletionsCompat` and `AnthropicMessagesCompat`.
- Updated `detectCompat` with nvidia/ant-ling non-standard flags, ant-ling thinking format, and OpenRouter developer-role model-prefix restriction.
- Regenerated `models_generated.go` from v0.78.1 (968 models / 35 providers).
- Regenerated `images/models_generated.go` from v0.78.1 (30 image models / 1 provider).
- Updated compat test expectations for new OpenRouter behavior.
- Re-ran full validation suite.

Result: upstream v0.78.1 is fully synced in Go with provider, compat, registry, and image model parity.

### v0.78.0 (2026-05-30)

Comparative audit (`@earendil-works/pi-ai v0.77.0` → `v0.78.0`) found:

- Upstream package version advanced to `v0.78.0`; the generated model registry remained unchanged at 931 models across 32 providers.
- The relevant runtime/code diffs were concentrated in provider auth handling: OpenAI completions, OpenAI Responses, OpenAI Codex, Anthropic, Google, and Mistral now require an explicit API key in the provider runtime instead of falling back to environment lookup.
- Upstream also tightened the error string to `No API key for provider: …` in these paths.
- `dist/types.d.ts` only changed documentation wording for custom headers and the `thinkingFormat` comment; no new type surface or model metadata landed in this patch.
- No changes were found in headers/base URLs, streaming/event parsing, OAuth/auth flows, reasoning/thinking maps, Codex transport/cache behavior, or the generated model registry.

Actions:

- Removed environment-key fallback from `ResolveAPIKey()` so provider streams now require explicit keys, matching upstream 0.78.0 behavior.
- Updated provider error strings to match upstream’s `No API key for provider: …` wording.
- Re-ran targeted parity tests for API-key resolution and provider error handling.
- Kept `models_generated.go` unchanged because the upstream registry did not change.

Result: upstream v0.78.0 is synced in Go; the only Go-facing change was the auth/runtime parity update plus audit/doc updates.

### v0.77.0 (2026-05-29)

Comparative audit (`@earendil-works/pi-ai v0.76.0` → `v0.77.0`) found:

- Upstream refreshed the generated model registry from 923 to 931 models across 32 providers.
- Model catalog changes included new Claude Opus 4.8 entries across regional variants and expanded reasoning metadata, including the new `thinkingFormat: "string-thinking"` path and additional `minimal: null` thinking-map entries.
- `dist/types.d.ts` added the `string-thinking` compat option and the Anthropic `allowEmptySignature` compat flag.
- Provider/runtime diffs were limited to wire-format parity updates: OpenAI completions gained `string-thinking`, OpenAI Responses replay now uses stable fallback IDs for text items, and Anthropic-compatible replay now preserves empty thinking signatures.
- No other parity-relevant deltas were found in headers, base URLs, streaming/event parsing, OAuth refresh behavior, reasoning maps, or Codex transport/cache handling.
- Upstream did not publish release notes for this patch, so the audit relied on direct package/code comparison.

Actions:

- Regenerated `models_generated.go` from v0.77.0 to keep the Go model registry aligned with upstream metadata.
- Ported the new `string-thinking` completions behavior, empty-signature replay handling, and Responses replay ID fallback parity.
- Added regression tests for the new OpenAI completions thinking format and Responses replay behavior.
- Re-ran the full validation suite after the sync.

Result: upstream v0.77.0 is synced in Go; the Go-facing changes are the regenerated model registry plus the parity fixes and audit/doc updates.

### v0.76.0 (2026-05-28)

Comparative audit (`@earendil-works/pi-ai v0.75.5` → `v0.76.0`) found:

- Upstream refreshed the generated model registry from 924 to 923 models across 32 providers.
- Model catalog changes were limited to metadata: pricing updates across existing entries, removal of `cerebras/qwen-3-235b-a22b-instruct-2507`, addition of `opencode/mimo-v2.5-free`, and addition of `opencode-go/qwen3.7-max`.
- No parity-relevant changes were found in `dist/types.d.ts`, provider payload builders, header/base-URL resolution, streaming/event parsing, OAuth/auth behavior, reasoning/thinking maps, or Codex transport/cache behavior.
- Release notes were not published upstream for this patch, so the audit relied on direct code and registry diffing.

Actions:

- Regenerated `models_generated.go` from v0.76.0 to keep the Go model registry aligned with upstream metadata.
- Verified the Go runtime/provider surface already matched upstream for the audited release; no provider, header, stream, OAuth, or Codex changes were required.
- Re-ran the full validation suite after regeneration.

Result: upstream v0.76.0 is synced in Go; the only Go-facing change is the regenerated model registry and the accompanying audit/doc updates.

## Sync history

### v0.75.5 (2026-05-25)

Comparative audit (`@earendil-works/pi-ai v0.75.4` → `v0.75.5`) found:

- Upstream refreshed the generated model registry: `models.generated.js` dropped from 944 to 924 models and remained at 32 providers.
- New/changed model metadata included Cloudflare Workers AI additions and capability tweaks, including `compat.forceAdaptiveThinking` on Anthropic-compatible built-ins and a set of Workers AI model additions/renames.
- `dist/index.d.ts` now re-exports `OAuthDeviceCodeInfo`, and the bundled CLI sources gained `onDeviceCode`/`onSelect` login callbacks for interactive OAuth flows.
- `package.json` added `@smithy/node-http-handler`, which affects upstream build/runtime dependencies but does not change Go library behavior.

Actions:

- Regenerated `models_generated.go` from v0.75.5 to keep the Go model registry aligned with upstream metadata.
- Verified the Go code already supports the relevant model compat/path behavior for the changed metadata; no provider payload, header, streaming, or OAuth runtime changes were required.
- Documented the CLI/OAuth type-surface delta as an intentional divergence because go-ai does not embed the upstream Node CLI layer.
- Re-ran the full validation suite after regeneration.

Result: upstream v0.75.5 is synced in Go; the only Go-facing change is the regenerated model registry and the accompanying audit/doc updates.

## Sync history

### v0.75.3 (2026-05-19)

Comparative audit (`@earendil-works/pi-ai v0.75.2` → `v0.75.3`) found:

- Upstream changed only `package.json` metadata/version.
- `dist/models.generated.js`, `dist/image-models.generated.js`, type declarations, provider runtime files, image APIs, OAuth/header/event parsing surfaces, and `simple-options` were byte-identical to `v0.75.2`.
- No Go code or generated metadata changes were required after the recent `images/`, `inference/`, and `transports/` package layout split.

Validation gates passed; tag `v0.75.3` marks the audited no-op sync point.

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
