# Audit Report

Final audit snapshot after the current hardening pass.

## What is in good shape

- Unified provider API (`Stream` / `Complete`) works across implemented providers.
- HTTP providers honor request hooks and opt-in retry configuration.
- SSE transport failures now surface as `ErrorEvent` instead of silently ending.
- OpenAI Codex supports:
  - WebSocket transport
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
