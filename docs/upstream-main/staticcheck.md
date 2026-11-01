# Root staticcheck

The root module passes staticcheck v0.8.2 with CGO disabled and enabled on Go 1.27.3 (`GOMAXPROCS=4`). The initial run reproduced all 23 recorded findings. This closes the root static-analysis gate; semantic acceptance and pre-release profiling remain incomplete.

## SQLite helper build modes

CGO-disabled verification during the idle-retention work found another U1000: `lockStorageFile` was compiled on Unix without its sole caller, the CGO-only SQLite backend. Both platform helper files now use the caller's `cgo` constraint. The helper body, file-permission checks, symlink fence, lock handling and non-Unix error are unchanged. Both CGO modes pass full root tests, vet and pinned staticcheck again. No ignore was added.

CGO-disabled Windows/amd64 library and Darwin/arm64 test-binary crossbuilds pass. The Windows test binary is blocked by five existing, untagged `syscall.Kill` calls in `journal_recovery_test.go`, `lifecycle_test.go` and `storage_conformance_test.go`. Neither crossbuild was executed; Windows test portability needs a separate fix. Disposable binaries were removed.

## Changes

Four unused private declarations were removed: `durable.isNativeUsageType`, Mistral's `mistralToolCallIDLength`, Codex's `buildCodexInput` and OpenAI Responses' `extractText`. The production call paths already used other implementations. The same-named Anthropic text helper remains in use and is unchanged.

The Codex WebSocket fallback test now uses `http.StatusUpgradeRequired` for its existing 426 response.

Eighteen statement-local exceptions retain existing behaviour. No file-wide or check-wide exclusions were added, and no diagnostic strings changed.

| Check | Locations | Reason |
| --- | --- | --- |
| ST1005, 1 | `inference/provider/bedrock/bedrock.go` | Bedrock is a proper noun in the existing missing-stream diagnostic. |
| ST1005, 6 | `inference/provider/mistral/content.go` | Preserve public malformed-content diagnostics. |
| ST1005, 5 | `inference/provider/mistral/mistral.go` | Preserve public malformed streaming-event, delta and tool-call diagnostics. |
| ST1005, 2 | `inference/provider/openaicodex/codex.go` | Preserve unfinished-tool diagnostics on both transports. |
| ST1005, 1 | `inference/provider/openaicodex/payload.go` | Codex is a proper noun in the existing payload diagnostic. |
| ST1005, 1 | `inference/provider/openairesponses/responses.go` | Preserve the unfinished-tool diagnostic. |
| ST1005, 1 | `internal/responsestream/terminal.go` | Preserve the shared unknown-status diagnostic. |
| SA9005, 1 | `internal/jsonparse/arguments_stream_main_test.go` | Deliberate negative test: private stream state must expose no JSON fields. The marshal-error and empty-object assertions remain unchanged. |

Keeping native diagnostic text stable does not establish upstream error-domain parity. The existing unfinished-tool divergence and observer/reference decisions still need Rui's acceptance or implementation work.

## Verification

After sourcing `scripts/project-env.sh`, with `GOTOOLCHAIN=go1.27.3 GOMAXPROCS=4`:

- `go test ./... -count=1 -timeout=600s`, `go vet ./...` and `go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...` pass in each CGO mode.
- Full affected-package race tests pass three repetitions per shuffle seed 7 and 42: Bedrock, Mistral, Codex, OpenAI Responses, `internal/jsonparse` and `internal/responsestream`.
- Durable usage tests (`-run '^TestUsage'`) pass the same race/shuffle repetitions.
- `go mod verify` and `git diff --check` pass.

Root tests used empty project-owned HOME/APPDATA directories with Google credential-path/quota overrides unset. Those directories were removed after use. No new profiles or disposable run logs were retained. No dependency, exported API, storage format, model asset or release ref changed. Session grouping and overlap remain disabled.
