# Audit Report

Final audit snapshot after the current hardening pass.

## 2026-05-01 upstream structure/payload audit

Compared `go-ai` against `@mariozechner/pi-ai` v0.70.3 through v0.72.0, with emphasis on model payloads and API metadata. The audit found and fixed the main high-risk gaps introduced by the recent upstream releases:

- **OpenAI-compatible response metadata**: `responseModel` is now parsed from streamed chat-completion chunks when providers report a model ID that differs from the requested model. Response IDs are captured from chunk IDs.
- **OpenAI-compatible usage normalization**: cached-token accounting now supports both `prompt_tokens_details.cached_tokens` and provider-specific `prompt_cache_hit_tokens`, and separates cache reads from cache writes.
- **Cloudflare AI Gateway / Workers AI**: OpenAI Completions, OpenAI Responses, and Anthropic providers now resolve `{CLOUDFLARE_*}` base-URL placeholders at request time. Cloudflare AI Gateway uses `cf-aig-authorization` instead of a normal provider `Authorization`/`X-Api-Key` header.
- **Provider-first OpenAI compat detection**: compatibility inference now mirrors pi-ai's recent provider-first approach for Moonshot, Cloudflare Workers AI, Cloudflare AI Gateway, DeepSeek, OpenRouter, Ollama, etc., and merges explicit `Model.CompletionsCompat` overrides.
- **Cache retention payload fields**: OpenAI Completions and Responses now emit prompt-cache key/retention fields when the caller opts into cache retention and the provider supports the long-retention variant.
- **Mistral reasoning payloads**: `mistral-small-2603`, `mistral-small-latest`, and `mistral-medium-3.5` now use `reasoning_effort` instead of generic `prompt_mode=reasoning`.
- **Anthropic stream integrity**: Anthropic streams that start a message but end without `message_stop` now surface an `ErrorEvent` instead of silently completing.
- **OpenAI Codex cached WebSocket transport**: `websocket-cached` reuses session-scoped Codex WebSockets and sends continuation deltas with `previous_response_id`, with exported debug/session helpers.
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
