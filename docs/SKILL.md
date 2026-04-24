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

### Step 7: compare docs and residual gaps

Update these when needed:
- `README.md`
- `docs/basic-usage.md`
- `docs/context-hooks.md`
- `docs/HARNESS.md`
- `docs/model-selection.md`
- `docs/GAP_ANALYSIS.md`
- `docs/TEST_MATRIX.md`
- `docs/AUDIT_REPORT.md`

### Step 8: validate end to end

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

## Required outputs of a sync pass

A correct sync pass should usually do **all** of these when appropriate:

1. Update code if upstream changed
2. Regenerate `models_generated.go` if model metadata changed
3. Update `docs/GAP_ANALYSIS.md` to the new upstream version
4. Record whether the bump was:
   - metadata-only
   - docs-only
   - behavioral/API-affecting
5. Run tests and vet
6. Push commits
7. **Tag the release to match upstream**: `git tag -a vX.Y.Z -m "Sync with upstream pi-ai vX.Y.Z"` and `git push origin vX.Y.Z`

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
