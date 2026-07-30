# Release parity record

This file is the release-audit source of truth for `github.com/rcarmo/go-ai` parity with upstream `@earendil-works/pi-ai`.

## Current upstream baseline

- Upstream package: `@earendil-works/pi-ai`
- Current audited release: `v0.83.0`
- Upstream tag/SHA: `845d6ff1f6643aba440341cce877ce1c43ebbc39`
- Previous accepted baseline: `v0.82.1` / `b4f293684bba718d59cc1157679bcf6157b3a7f5`
- Go parity commits for this release:
  - `63e27f4` — `Sync pi-ai v0.83.0 parity`
  - `9976049` — `Complete v0.83 runtime stop and OAuth fixes`
  - `2f17e7b` — `Complete v0.83 pending stop reason semantics`
- Exact upstream checkout used: `/workspace/tmp/pi-v0830`
- Exact previous checkout used: `/workspace/tmp/pi-v0821`
- Detailed path matrix: `docs/v0830-release-ledger.md`

## Exact upstream changes audited

Release-only diff: `packages/ai` from `v0.82.1` to `v0.83.0`, no later `main` changes.

Material upstream deltas:

1. Generated text model catalog refresh.
2. Raw provider stop reason preservation across streaming providers.
3. Pending stop reason as a public/breaking stop reason value.
4. Missing terminal stop/status handling: streams that never produce a terminal provider stop reason/status must not default to successful `stop`.
5. Bedrock credential precedence updates for explicit/scoped profiles vs ambient AWS keys.
6. JavaScript `fetch` option plumbing across TS adapters.
7. Test/fixture updates for provider stop reasons and catalog metadata.

## Go implementation and decisions

| Upstream delta | Go disposition |
| --- | --- |
| Text model catalog refresh | Implemented. `models_generated.go` regenerated from exact `v0.83.0` upstream model data. Comparator: `1153/1153` provider/id pairs. Metadata regressions updated for current OpenRouter Kimi/GLM values and current retained/removed model IDs. |
| Image catalog | Compared and unchanged in count. Exact image comparator: `40/40` provider/id pairs. |
| Public pending stop reason | Implemented. Added `goai.StopReasonPending` with JSON serialization coverage in `stop_reason_pending_test.go`. |
| Raw stop reason preservation | Implemented. Added `Message.RawStopReason`; populated for OpenAI Completions, Mistral, Google, Bedrock, Anthropic, OpenAI Responses/Azure-shared, and Codex stream paths. |
| Missing terminal stop/status must not become success | Implemented. Anthropic, Responses/Azure-shared, Codex, OpenAI Completions, Mistral, Google, and Bedrock partial messages initialize as pending where applicable and emit an error on missing terminal stop/status instead of successful `Done`. |
| Anthropic pending/raw semantics | Implemented. `processAnthropicStream` initializes pending, sets raw stop reason from `message_delta.delta.stop_reason`, and errors if `message_stop`/end occurs without a stop reason. Tests: `anthropic_raw_stop_reason_test.go` plus adjusted request-capture fixtures. |
| Responses/Azure/Codex raw status and pending terminal behavior | Implemented. Responses parser sets raw status from terminal response status; missing terminal response event errors. Codex parser mirrors this. Tests: `raw_status_upstream_test.go`, existing Codex custom stream tests. |
| Bedrock raw stop reason | Implemented. Converse stream message stop now records raw Bedrock stop reason and maps unknown provider stops to error with provider message. Test: `TestProcessConverseStreamPreservesRawStopReason`. |
| OpenAI malformed tool delta (`function` plus empty `custom`) | Implemented. Function arguments are preserved. Test: `openai_malformed_tool_delta_test.go`. |
| OAuth minimum-validity refresh | Implemented during v0.83 corrective pass. `oauth.GetAPIKey` refreshes tokens inside the default 5-minute validity window. `GetAPIKeyWithMinValidity` supports stricter callers and rejects too-short refreshed tokens. Tests in `oauth/oauth_test.go`. |
| Bedrock credential precedence | Implemented. Explicit `StreamOptions.Profile` and scoped `ProviderEnv{"AWS_PROFILE": ...}` take precedence over ambient AWS access keys; ambient profile plus ambient keys remains compatible. Covered by Bedrock credential tests and existing endpoint/option tests. |
| JavaScript `fetch` option plumbing | N/A/adapted. Go does not use JS `fetch`; request customization is via explicit HTTP clients/transports, retry configuration, payload/response hooks, and provider env/options. Existing Go HTTP request paths and retry/provider tests cover the equivalent Go surfaces. |
| TypeScript SDK/mock harness changes | N/A unless they expose Go-facing behavior above. Classified in `docs/v0830-release-ledger.md`. |

## Validation evidence

Final v0.83 corrective gate passed before commit `2f17e7b`:

```text
PI_AI_MODEL_DATA_DIR=/workspace/tmp/pi-v0830-json/providers python3 scripts/compare-upstream-models.py /workspace/tmp/pi-v0830/packages/ai/src/providers
# upstream pairs: 1153
# generated pairs: 1153
# model provider/id pairs match exactly

# image comparator against /workspace/tmp/pi-v0830/packages/ai/src/image-models.generated.ts
# upstream image pairs: 40
# generated image pairs: 40
# image provider/id pairs match exactly

make check
TMPDIR=/workspace/tmp go test -shuffle=on ./...
TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1
go vet ./...
make staticcheck
make check-logging
make test-repro
```

All listed gates passed.

## Maintenance policy

For every future upstream release audit, update this file in the same release commit before declaring completion. The update must include:

- upstream release tag and SHA;
- previous accepted baseline tag and SHA;
- exact checkout paths or reproducible source locations;
- changed-path matrix link;
- catalog comparator counts;
- every Go implementation, fix, adaptation, and N/A decision;
- tests and full gate evidence.
