---
name: go-ai-upstream-sync
description: Sync go-ai with upstream @mariozechner/pi-ai changes — audit upstream version deltas, regenerate models, port API/type/provider changes, update docs, and validate parity.
distribution: private
---

# go-ai Upstream Sync

Use this skill when `@mariozechner/pi-ai` has been updated and `go-ai` needs to track those changes.

## When to run

- After an upstream `pi-ai` version bump
- When new models/providers appear upstream
- When upstream `types.d.ts` changes
- When OAuth/login flows change upstream
- When provider behavior or compatibility flags change

## Ground rules

- Read the current upstream package from the installed path:
  - `/usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai`
- Treat the current `go-ai` repo state as canonical for file layout.
- Do **not** assume old file names from earlier refactors (`overflow.go`, `validation.go`, `sanitize.go`, `copilot_headers.go`, `generate-models.ts`, etc.).
- Validate with `go test ./...` and `go vet ./...` before pushing.
- For **every** upstream release, perform a complete comparative audit against upstream code and `go-ai` code before declaring the release synced. Do not treat any release as “just a diff” without this audit.

## Current go-ai structure

Important current files:

- `types.go`
- `events.go`
- `registry.go`
- `context.go`
- `transform.go`
- `harness.go`
- `env.go`
- `compat.go`
- `retry.go`
- `logger.go`
- `azure.go`
- `simple_options.go`
- `utils.go`
- `models_generated.go`
- `doc.go`
- `provider/*/`
- `oauth/*.go`
- `scripts/generate-models.go`
- `scripts/check-logging.sh`
- `docs/GAP_ANALYSIS.md`
- `docs/TEST_MATRIX.md`
- `docs/AUDIT_REPORT.md`

## Pre-flight checks

### 1. Confirm current upstream version

```bash
cat /usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai/package.json | jq -r .version
```

### 2. Check current go-ai sync marker

```bash
cd /workspace/projects/go-ai
sed -n '1,20p' docs/GAP_ANALYSIS.md
sed -n '1,5p' models_generated.go
```

### 3. Check working tree before sync

```bash
cd /workspace/projects/go-ai
git status --short
```

## Sync workflow

### Step 1: inspect upstream surface area

```bash
PI_AI=/usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai
find "$PI_AI/dist" -maxdepth 3 -type f | sort
```

Read at minimum:
- `package.json`
- `README.md`
- `dist/index.d.ts`
- `dist/types.d.ts`
- `dist/api-registry.d.ts`
- `dist/providers/register-builtins.d.ts`
- relevant `dist/providers/*.js` / `.d.ts`
- `dist/oauth.d.ts`

### Step 2: regenerate the model registry

Use the **Go** generator, not the old TS one.

```bash
cd /workspace/projects/go-ai
go run scripts/generate-models.go
go test ./... -count=1
go vet ./...
```

This picks up:
- new models
- removed models
- pricing changes
- context window changes
- provider ID changes in generated metadata

### Step 3: compare types and provider metadata

Compare upstream `dist/types.d.ts` against:
- `types.go`
- `events.go`
- `compat.go`
- `env.go`
- `simple_options.go`

Look for:
- new `KnownApi` values
- new `KnownProvider` values
- new `StreamOptions` fields
- new content block fields
- new compat flags
- changed provider metadata names
- changed stop reasons / transport types / cache retention values

### Step 4: compare API registry behavior

Compare upstream registry/types against:
- `registry.go`
- provider `init()` registrations

Look for:
- new built-in provider modules
- new aliases
- changed stream/simple-stream contracts

### Step 5: compare providers

For each upstream provider under `dist/providers/`, compare to the Go implementation under `provider/`.

Current provider mapping:

| go-ai | upstream |
|---|---|
| `provider/openai/` | `dist/providers/openai-completions.*` |
| `provider/openairesponses/` | `dist/providers/openai-responses.*` |
| `provider/openaicodex/` | `dist/providers/openai-codex-responses.*` |
| `provider/anthropic/` | `dist/providers/anthropic.*` |
| `provider/google/` | `dist/providers/google.*` |
| `provider/geminicli/` | `dist/providers/google-gemini-cli.*` |
| `provider/mistral/` | `dist/providers/mistral.*` |
| `provider/bedrock/` | `dist/bedrock-provider.*` and/or bedrock provider module |
| `provider/faux/` | `dist/providers/faux.*` |

Check for:
- new payload fields
- new headers/session behavior
- retry/reconnect changes
- SSE/WS transport behavior
- Azure-specific normalization / trimming behavior
- tool-call streaming changes
- image handling changes
- reasoning/thinking behavior changes

### Step 6: compare OAuth providers

Compare upstream OAuth surface against `oauth/*.go`.

Check for:
- new provider IDs
- changed endpoints/client IDs/scopes
- new login flows
- changed token refresh behavior
- `ModifyModels()` behavior changes

### Step 7: complete comparative audit pass

After the mechanical per-version sync, do a deliberate second pass over the upstream release code and the current `go-ai` code. This is **mandatory for every upstream release**, including metadata-only, docs-only, patch, and no-op-looking releases. Never declare a release synced until this audit is complete.

The audit must compare the upstream release artifacts/code against the Go implementation, not only the changelog. Start from the release notes when available, then verify each note and any actual code diff against `go-ai`.

Audit at least:

- **Type/API surface**
  - `dist/types.d.ts`, `dist/index.d.ts`, `dist/api-registry.d.ts`
  - `KnownApi`, `KnownProvider`, message fields such as `responseId` / `responseModel`
  - `StreamOptions` / simple options changes (`serviceTier`, `reasoningSummary`, `timeoutMs`, `maxRetries`, cache/session options)
- **Model payload construction**
  - request fields added/removed/renamed (`max_tokens`, `max_completion_tokens`, `max_output_tokens`, `prompt_cache_key`, `prompt_cache_retention`, `reasoning`, `reasoning_effort`, provider-specific `thinking` blocks)
  - tool payload shape (`strict`, tool stream flags, tool result names, function call ID normalization)
  - image/tool-result replay behavior
- **Provider headers and base URLs**
  - API key header changes (`Authorization`, `X-Api-Key`, `cf-aig-authorization`, Copilot dynamic headers)
  - session/cache affinity headers
  - placeholder base URL resolution (e.g. Cloudflare `{CLOUDFLARE_*}`)
  - Azure/OpenAI/Anthropic URL normalization differences
- **Streaming/event parsing**
  - response ID/model capture
  - usage/cache token normalization (`cached_tokens`, `cache_write_tokens`, `prompt_cache_hit_tokens`)
  - stop reason mapping
  - incomplete stream detection (e.g. Anthropic `message_start` without `message_stop`)
  - SSE/WS retry and error propagation changes
- **Compatibility detection**
  - provider-first vs URL-based detection
  - explicit model compat overrides
  - provider quirks for OpenRouter, DeepSeek, Moonshot, Cloudflare Workers AI / AI Gateway, Ollama, Groq, Cerebras, xAI, z.ai, Vercel AI Gateway, Qwen/DashScope, Chutes, OpenCode
- **Reasoning/thinking behavior**
  - simple option mapping and clamping
  - provider-specific reasoning effort maps
  - Mistral `promptMode` vs `reasoningEffort`
  - Bedrock/Anthropic adaptive thinking and model-name matching
  - Google thought signatures and cross-provider replay behavior
- **OAuth/login and model mutation**
  - removed/deprecated providers that should remain in Go for backward compatibility
  - `ModifyModels()` equivalents and token refresh behavior

Practical diff commands:

```bash
PREV=/tmp/pi-ai-prev/package/dist
NEW=/tmp/pi-ai-new/package/dist

# Surface-level deltas
diff -u "$PREV/types.d.ts" "$NEW/types.d.ts" | sed -n '1,220p'
diff -u "$PREV/providers/openai-completions.js" "$NEW/providers/openai-completions.js" | sed -n '1,260p'
diff -u "$PREV/providers/openai-responses.js" "$NEW/providers/openai-responses.js" | sed -n '1,260p'
diff -u "$PREV/providers/anthropic.js" "$NEW/providers/anthropic.js" | sed -n '1,260p'
diff -u "$PREV/providers/mistral.js" "$NEW/providers/mistral.js" | sed -n '1,220p'
diff -u "$PREV/providers/google-shared.js" "$NEW/providers/google-shared.js" | sed -n '1,220p'
diff -u "$PREV/providers/amazon-bedrock.js" "$NEW/providers/amazon-bedrock.js" | sed -n '1,220p'
diff -u "$PREV/env-api-keys.js" "$NEW/env-api-keys.js" | sed -n '1,160p'

# Focused searches in the new upstream tree
grep -R "responseModel\|prompt_cache\|cache_write\|prompt_cache_hit\|cf-aig\|reasoningEffort\|reasoning_effort\|message_stop\|baseURL\|supportsStrictMode\|maxTokensField" "$NEW"/providers "$NEW"/*.js
```

For every release and every gap found, either:

1. port the behavior,
2. add/adjust tests proving Go already matches it, or
3. document an intentional divergence in `docs/AUDIT_REPORT.md` and `docs/GAP_ANALYSIS.md`.

The final response/commit notes must explicitly say the complete comparative audit was performed and summarize the result.

Add fake-server or parser tests for high-risk payload/API changes, especially:

- provider-specific headers and URL resolution
- request body fields and compat flags
- response metadata (`responseId`, `responseModel`)
- cache usage normalization
- stop reason and incomplete-stream handling

### Step 8: compare docs and residual gaps

Update these when needed:
- `README.md`
- `docs/basic-usage.md`
- `docs/context-hooks.md`
- `docs/HARNESS.md`
- `docs/model-selection.md`
- `docs/GAP_ANALYSIS.md`
- `docs/TEST_MATRIX.md`
- `docs/AUDIT_REPORT.md`

### Step 9: validate end to end

```bash
cd /workspace/projects/go-ai
go test ./... -count=1
go vet ./...
go build ./examples/...
./scripts/check-logging.sh
```

Optional extra pass:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | sort -k3 | sed -n '1,120p'
```

## Sync granularity

**Always sync one patch release at a time, in order.**

Do not skip versions. If upstream went from v0.70.0 to v0.70.3, sync v0.70.1 first, then v0.70.2, then v0.70.3 — each as a separate commit + tag.

Why:
- Intermediate releases may introduce types, flags, or provider changes that later releases depend on.
- Jumping versions risks merging conflicting changes that are hard to debug.
- Per-release commits make `git bisect` and rollback straightforward.
- Each tag is a known-good state that matches an exact upstream release.

Workflow when multiple releases are pending:

```bash
# Check what's available
npm view @mariozechner/pi-ai versions --json | tail -n 10

# Download each release tarball individually
curl -sSL "https://registry.npmjs.org/@mariozechner/pi-ai/-/pi-ai-X.Y.Z.tgz" -o /tmp/pi-ai-X.Y.Z.tgz

# Diff against previous, port, test, commit, tag — then repeat for next version
```

## Required outputs of a sync pass

A correct sync pass should usually do **all** of these when appropriate:

1. Update code if upstream changed
2. Regenerate `models_generated.go` if model metadata changed
3. Run the complete comparative audit pass for **every** upstream release
4. Add or update tests for any high-risk payload/header/streaming changes, or tests proving parity when behavior is already covered
5. Update `docs/GAP_ANALYSIS.md` to the new upstream version
6. Update `docs/AUDIT_REPORT.md` when the deep audit finds residual gaps or intentional divergences
7. Record whether the bump was:
   - metadata-only
   - docs-only
   - behavioral/API-affecting
8. Run tests, vet, and build
9. Push commits
10. **Tag the release to match upstream**: `git tag -a vX.Y.Z -m "Sync with upstream pi-ai vX.Y.Z"` and `git push origin vX.Y.Z`

## Provider implementation checklist

When adding or updating a provider:

- register the API in `init()`
- support `OnPayload` if the provider sends request payloads directly
- support `OnResponse` where HTTP response headers exist
- wire `RetryConfig` for HTTP-based providers
- ensure SSE/WS failures surface as `ErrorEvent`
- add logging at key points
- add provider-level fake-server tests where practical

## Logging checklist

At minimum for HTTP providers:

- `Debug("stream start", ...)`
- `Debug("HTTP request", ...)`
- `Warn("HTTP error response", ...)`
- `Debug("request aborted", ...)` or `Warn("network error", ...)`

For retry/reconnect-sensitive paths:
- log retry attempts
- surface mid-stream errors as `ErrorEvent`
- document reconnect expectations in docs if behavior changed

## Commit guidance

Use a commit like:

```text
Sync upstream pi-ai vX.Y.Z

- regenerated model registry
- updated provider metadata parity
- ported [specific provider/type/oauth] changes
- updated docs/GAP_ANALYSIS.md
```

If the bump is metadata-only, say so explicitly in the commit body and in `docs/GAP_ANALYSIS.md`.
