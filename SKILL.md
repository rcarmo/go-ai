---
name: go-ai-upstream-sync
description: Sync go-ai with upstream @mariozechner/pi-ai changes — regenerate models, port new providers, update types, and validate parity.
distribution: private
---

# go-ai Upstream Sync

Use this skill when `@mariozechner/pi-ai` has been updated and go-ai needs to track those changes.

## When to run

- After a pi-ai version bump (check `package.json` version in the installed package)
- When new providers or models are added upstream
- When type definitions change
- When new OAuth flows are added
- Periodically as a maintenance sweep

## Pre-flight checks

1. **Confirm current pi-ai version**:
   ```bash
   cat /usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai/package.json | jq .version
   ```

2. **Check go-ai's last synced version** (recorded in `models_generated.go` header):
   ```bash
   head -n 4 models_generated.go
   ```

3. **Compare module inventories** — find new/changed files:
   ```bash
   PI_AI=/usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai
   # List all JS modules with line counts
   find "$PI_AI/dist" -name '*.js' | while read f; do
       rel=$(echo "$f" | sed "s|$PI_AI/dist/||;s|\.js$||")
       lines=$(wc -l < "$f")
       printf "%-55s %5d\n" "$rel" "$lines"
   done | sort
   ```

## Sync steps

### Step 1: Regenerate model registry

```bash
cd /workspace/projects/go-ai
bun run scripts/generate-models.ts
go build ./...
go test ./... -count=1
```

This picks up:
- New models (names, IDs, costs, context windows)
- New providers
- Changed pricing or capabilities

### Step 2: Check for type changes

Compare pi-ai's `types.d.ts` against `types.go`:

```bash
PI_AI=/usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai
cat "$PI_AI/dist/types.d.ts"
```

Look for:
- New fields on `Message`, `Context`, `Tool`, `Model`, `StreamOptions`
- New content block types
- New event types in `AssistantMessageEvent`
- New `KnownProvider` or `KnownApi` values
- New `StopReason` values
- Changes to `OpenAICompletionsCompat`

Update `types.go` and `events.go` to match.

### Step 3: Check for new providers

```bash
PI_AI=/usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai
diff <(find "$PI_AI/dist/providers" -name '*.js' | sed 's|.*/||;s|\.js$||' | sort) \
     <(ls provider/ | sort)
```

For each new provider:
1. Read the `.d.ts` type definitions
2. Read the `.js` implementation
3. Create `provider/<name>/<name>.go`
4. Register in `init()` with the correct `Api` constant
5. Add to `scripts/check-logging.sh`
6. Add to README provider table

### Step 4: Check for OAuth changes

```bash
PI_AI=/usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai
diff <(find "$PI_AI/dist/utils/oauth" -name '*.js' | sed 's|.*/||;s|\.js$||' | sort) \
     <(ls oauth/*.go | sed 's|oauth/||;s|\.go$||' | sort)
```

Look for:
- New OAuth provider implementations
- Changed client IDs, scopes, or endpoints
- New OAuth flow types (e.g., new device flow variants)

### Step 5: Check for compat flag changes

```bash
PI_AI=/usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai
cat "$PI_AI/dist/types.d.ts" | sed -n '/OpenAICompletionsCompat/,/^}/p'
```

Compare against `compat.go` — add any new flags and update `DetectCompat()`.

### Step 6: Check for utility changes

```bash
PI_AI=/usr/local/lib/bun/install/global/node_modules/@mariozechner/pi-ai
# Overflow patterns
grep "OVERFLOW_PATTERNS" "$PI_AI/dist/utils/overflow.js" -A 30
# Env var mapping
grep "envMap" "$PI_AI/dist/env-api-keys.js" -A 30
```

Update `overflow.go` patterns and `env.go` mappings if changed.

### Step 7: Validate

```bash
cd /workspace/projects/go-ai
go build ./...
go test ./... -count=1
./scripts/check-logging.sh
# Optional: run fuzz tests
make fuzz FUZZTIME=30s
```

### Step 8: Update tracking

1. Update the `Generated:` date in `models_generated.go` header (automatic from generator)
2. Update the provider status table in `README.md`
3. Update `GAP_ANALYSIS.md` if new gaps were introduced
4. Commit with message format:
   ```
   Sync with pi-ai vX.Y.Z

   - Regenerated model registry (N models, M providers)
   - [list specific changes: new providers, type changes, etc.]
   ```

## Provider implementation pattern

When porting a new provider, follow this structure:

```go
package <name>

import (
    "context"
    goai "github.com/rcarmo/go-ai"
)

func init() {
    goai.RegisterApi(&goai.ApiProvider{
        Api:          goai.Api<Name>,
        Stream:       stream<Name>,
        StreamSimple: stream<Name>Simple,
    })
}

func stream<Name>Simple(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
    return stream<Name>(ctx, model, convCtx, opts)
}

func stream<Name>(ctx context.Context, model *goai.Model, convCtx *goai.Context, opts *goai.StreamOptions) <-chan goai.Event {
    ch := make(chan goai.Event, 32)
    go func() {
        defer close(ch)
        goai.GetLogger().Debug("stream start", "api", "<api-name>", "provider", model.Provider, "model", model.ID)
        // ... implementation
    }()
    return ch
}
```

Required logging points (enforced by quality gate):
1. `Debug("stream start", ...)` — entering the provider
2. `Debug("HTTP request", "url", ...)` — before HTTP call
3. `Warn("HTTP error response", "status", ...)` — non-200 status
4. `Debug("request aborted", ...)` or `Warn("network error", ...)` — failure paths

## Key file locations

| go-ai file | pi-ai source |
|---|---|
| `types.go` | `dist/types.d.ts` |
| `events.go` | `dist/types.d.ts` (AssistantMessageEvent) |
| `registry.go` | `dist/api-registry.js` + `dist/stream.js` |
| `env.go` | `dist/env-api-keys.js` |
| `compat.go` | `dist/types.d.ts` (OpenAICompletionsCompat) |
| `overflow.go` | `dist/utils/overflow.js` |
| `validation.go` | `dist/utils/validation.js` |
| `transform.go` | `dist/providers/transform-messages.js` |
| `simple_options.go` | `dist/providers/simple-options.js` |
| `sanitize.go` | `dist/utils/sanitize-unicode.js` |
| `models_generated.go` | `dist/models.generated.js` (via code gen) |
| `provider/openai/` | `dist/providers/openai-completions.js` |
| `provider/anthropic/` | `dist/providers/anthropic.js` |
| `provider/google/` | `dist/providers/google.js` + `google-shared.js` |
| `provider/mistral/` | `dist/providers/mistral.js` |
| `provider/bedrock/` | `dist/providers/amazon-bedrock.js` |
| `provider/openairesponses/` | `dist/providers/openai-responses.js` + `openai-responses-shared.js` |
| `provider/geminicli/` | `dist/providers/google-gemini-cli.js` + `google-shared.js` |
| `provider/openaicodex/` | `dist/providers/openai-codex-responses.js` |
| `oauth/` | `dist/utils/oauth/*.js` |
