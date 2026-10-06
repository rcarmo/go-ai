# Official 1.0.4 changed-path crosswalk

Rows audit the named native path and checks. Blanket TypeScript inner-subcase equivalence and live provider acceptance are unverified. Unchanged obligations retain the historical crosswalks.

## ai: 40 changed paths

| Status/path | Disposition | Native check |
| --- | --- | --- |
| M `packages/ai/CHANGELOG.md` | Release/package adaptation | `provenance.json`, native `go.mod`; JS dependency identity is separate |
| M `packages/ai/package.json` | Release/package adaptation | `provenance.json`, native `go.mod`; JS dependency identity is separate |
| M `packages/ai/scripts/generate-models.ts` | Pinned catalog/schema/native types | Generator, 1537/60/23 generated records, typed lookup tests; regeneration and deliberate corruption checks |
| A `packages/ai/src/api/azure-openai-config.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| M `packages/ai/src/api/azure-openai-responses.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| M `packages/ai/src/api/openai-completions.ts` | Sampling precedence/reasoning replay | `sampling.go`, `models_runtime.go`, providers; `sampling_test.go` and provider wire tests |
| M `packages/ai/src/api/openai-responses.ts` | Sampling precedence/reasoning replay | `sampling.go`, `models_runtime.go`, providers; `sampling_test.go` and provider wire tests |
| M `packages/ai/src/api/simple-options.ts` | Sampling precedence/reasoning replay | `sampling.go`, `models_runtime.go`, providers; `sampling_test.go` and provider wire tests |
| M `packages/ai/src/auth/resolve.ts` | Host-store atomic refresh adaptation | `oauth/stored_refresh.go`, `stored_refresh_test.go`; host-owned process-shared lock/persistence |
| M `packages/ai/src/env-api-keys.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| M `packages/ai/src/models.generated.ts` | Pinned catalog/schema/native types | Generator, 1537/60/23 generated records, typed lookup tests; regeneration and deliberate corruption checks |
| M `packages/ai/src/models.ts` | Pinned catalog/schema/native types | Generator, 1537/60/23 generated records, typed lookup tests; regeneration and deliberate corruption checks |
| M `packages/ai/src/providers/all.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| D `packages/ai/src/providers/azure-openai-responses.models.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| D `packages/ai/src/providers/azure-openai-responses.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| A `packages/ai/src/providers/azure.models.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| A `packages/ai/src/providers/azure.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| M `packages/ai/src/types.ts` | Pinned catalog/schema/native types | Generator, 1537/60/23 generated records, typed lookup tests; regeneration and deliberate corruption checks |
| M `packages/ai/src/utils/retry.ts` | Bedrock stalled-stream retry, quota precedence | `retry_assistant.go`, `tests/retry_assistant_test.go` |
| M `packages/ai/test/abort.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |
| M `packages/ai/test/azure-openai-base-url.test.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| A `packages/ai/test/azure-openai-completions.test.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| M `packages/ai/test/azure-openai-responses-reasoning-replay.test.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| M `packages/ai/test/azure-openai-tool-choice.test.ts` | Azure identity/config/deployment routing | `azure_config.go`, OpenAI/Responses providers, generated catalogs; `azure_104_test.go` production HTTP/SSE and generate/replay proofs |
| M `packages/ai/test/context-overflow.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |
| M `packages/ai/test/cross-provider-handoff.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |
| M `packages/ai/test/empty.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |
| M `packages/ai/test/image-tool-result.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |
| M `packages/ai/test/models-runtime.test.ts` | Pinned catalog/schema/native types | Generator, 1537/60/23 generated records, typed lookup tests; regeneration and deliberate corruption checks |
| M `packages/ai/test/openai-responses-namespace.test.ts` | Sampling precedence/reasoning replay | `sampling.go`, `models_runtime.go`, providers; `sampling_test.go` and provider wire tests |
| M `packages/ai/test/openai-responses-tool-result-images.test.ts` | Sampling precedence/reasoning replay | `sampling.go`, `models_runtime.go`, providers; `sampling_test.go` and provider wire tests |
| M `packages/ai/test/responseid.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |
| M `packages/ai/test/retry.test.ts` | Bedrock stalled-stream retry, quota precedence | `retry_assistant.go`, `tests/retry_assistant_test.go` |
| M `packages/ai/test/sampling-options.test.ts` | Sampling precedence/reasoning replay | `sampling.go`, `models_runtime.go`, providers; `sampling_test.go` and provider wire tests |
| M `packages/ai/test/stream.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |
| M `packages/ai/test/supports-xhigh.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |
| M `packages/ai/test/tokens.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |
| M `packages/ai/test/tool-call-without-result.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |
| M `packages/ai/test/total-tokens.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |
| M `packages/ai/test/unicode-surrogate.test.ts` | Existing native provider suite, Azure identity migration | Native `tests/` and provider HTTP/SSE suites pass; live credential variants are unverified |

## durable: 42 changed paths

| Status/path | Disposition | Native check |
| --- | --- | --- |
| M `packages/durable/CHANGELOG.md` | Release/docs/package adaptation | `docs/durable/`, provenance, native module |
| M `packages/durable/README.md` | Release/docs/package adaptation | `docs/durable/`, provenance, native module |
| M `packages/durable/docs/spec.md` | Release/docs/package adaptation | `docs/durable/`, provenance, native module |
| M `packages/durable/package.json` | Release/docs/package adaptation | `docs/durable/`, provenance, native module |
| A `packages/durable/src/env/decode.ts` | Decoder/positional scan | `binary_reader.go`, `read_binary.go`; 240 pinned reference cases and bounded decoder tests |
| M `packages/durable/src/env/index.ts` | Additive Go environment interfaces | `durable/filesystem.go`, `shell.go`, native tools/conformance tests; JS export identity adapted |
| A `packages/durable/src/env/line-scan.ts` | Decoder/positional scan | `binary_reader.go`, `read_binary.go`; 240 pinned reference cases and bounded decoder tests |
| A `packages/durable/src/env/node-watch.ts` | Explicit polling/reader/argv adapter | `file_watch.go`, binary/dir readers, shell; `filesystem_104_test.go` includes target-order/overlap identity preservation; `shell_104_test.go`; no native-event mode claim |
| M `packages/durable/src/env/node.ts` | Additive Go environment interfaces | `durable/filesystem.go`, `shell.go`, native tools/conformance tests; JS export identity adapted |
| M `packages/durable/src/harness/agent.ts` | Persisted UUIDv7 identity/fork/compaction | `provider_identity.go`, `generation.go`, `compaction.go`; identity and existing fork/compaction tests |
| M `packages/durable/src/harness/compaction.ts` | Persisted UUIDv7 identity/fork/compaction | `provider_identity.go`, `generation.go`, `compaction.go`; identity and existing fork/compaction tests |
| M `packages/durable/src/harness/generation.ts` | Copied settings/progress | `settings.go`, `harness.go`, progress helpers; `progress_104_test.go`, settings/registry tests |
| M `packages/durable/src/harness/harness.ts` | Copied settings/progress | `settings.go`, `harness.go`, progress helpers; `progress_104_test.go`, settings/registry tests |
| M `packages/durable/src/harness/output.ts` | Skipped-output decoder flush/retention | `tool.go`, `tool_output.go`; `tool_output_skip_test.go` and output/lifecycle suites |
| A `packages/durable/src/harness/provider.ts` | Persisted UUIDv7 identity/fork/compaction | `provider_identity.go`, `generation.go`, `compaction.go`; identity and existing fork/compaction tests |
| M `packages/durable/src/harness/tool.ts` | Skipped-output decoder flush/retention | `tool.go`, `tool_output.go`; `tool_output_skip_test.go` and output/lifecycle suites |
| M `packages/durable/src/harness/types.ts` | Copied settings/progress | `settings.go`, `harness.go`, progress helpers; `progress_104_test.go`, settings/registry tests |
| M `packages/durable/src/harness/view.ts` | Copied settings/progress | `settings.go`, `harness.go`, progress helpers; `progress_104_test.go`, settings/registry tests |
| M `packages/durable/src/index.ts` | Go package export adaptation | `durable`, `durable/tools`; external-module import check |
| A `packages/durable/src/testing/env-conformance.ts` | Additive Go environment interfaces | `durable/filesystem.go`, `shell.go`, native tools/conformance tests; JS export identity adapted |
| M `packages/durable/src/testing/index.ts` | Native runner adaptation | Go task/storage/test fixtures; JavaScript test factory/export identities are not Go API |
| M `packages/durable/src/testing/runner.ts` | Native runner adaptation | Go task/storage/test fixtures; JavaScript test factory/export identities are not Go API |
| M `packages/durable/src/testing/types.ts` | Native runner adaptation | Go task/storage/test fixtures; JavaScript test factory/export identities are not Go API |
| M `packages/durable/src/tools/bash.ts` | Bash/PowerShell direct argv | `bash_portable_unix.go`, `powershell_unix.go`; shell/PowerShell/Bash tests; live PowerShell/Windows unverified |
| M `packages/durable/src/tools/image.ts` | Native MIME checks | `image.go`, `image_detect.go`; bounded PNG chunk walk and late-APNG/block-edge/IDAT-order/read-error tests in `image_detect_test.go`; native supported formats explicit |
| M `packages/durable/src/tools/index.ts` | Go package export adaptation | `durable`, `durable/tools`; external-module import check |
| M `packages/durable/src/tools/read.ts` | Bounded read/full-file totals | `read_binary.go`, `binary_reader.go`, `truncate.go`; read reference/bounded/240 scanner cases; integral native args |
| M `packages/durable/src/truncate.ts` | Prefix truncation with exact whole-selection totals | Exported `TruncateHeadOf`/`TruncationTotals` in `truncate.go`, used by `read_binary.go`; 80 pinned upstream fixtures and exact-2000-newline production boundary test, alongside 240 scanner cases |
| A `packages/durable/test/env-line-scan.test.ts` | Decoder/positional scan | `binary_reader.go`, `read_binary.go`; 240 pinned reference cases and bounded decoder tests |
| A `packages/durable/test/env-node-conformance.test.ts` | Explicit polling/reader/argv adapter | `file_watch.go`, binary/dir readers, shell; `filesystem_104_test.go`, `shell_104_test.go`; no native-event mode claim |
| M `packages/durable/test/env-node.test.ts` | Explicit polling/reader/argv adapter | `file_watch.go`, binary/dir readers, shell; `filesystem_104_test.go`, `shell_104_test.go`; no native-event mode claim |
| M `packages/durable/test/harness-compaction.test.ts` | Persisted UUIDv7 identity/fork/compaction | `provider_identity.go`, `generation.go`, `compaction.go`; identity and existing fork/compaction tests |
| M `packages/durable/test/harness-conversations.test.ts` | Persisted UUIDv7 identity/fork/compaction | `provider_identity.go`, `generation.go`, `compaction.go`; identity and existing fork/compaction tests |
| M `packages/durable/test/harness-generation.test.ts` | Copied settings/progress | `settings.go`, `harness.go`, progress helpers; `progress_104_test.go`, settings/registry tests |
| A `packages/durable/test/harness-output-skip.test.ts` | Skipped-output decoder flush/retention | `tool.go`, `tool_output.go`; `tool_output_skip_test.go` and output/lifecycle suites |
| M `packages/durable/test/harness-output.test.ts` | Skipped-output decoder flush/retention | `tool.go`, `tool_output.go`; `tool_output_skip_test.go` and output/lifecycle suites |
| M `packages/durable/test/harness-registry.test.ts` | Copied settings/progress | `settings.go`, `harness.go`, progress helpers; `progress_104_test.go`, settings/registry tests |
| M `packages/durable/test/harness-tools.test.ts` | Skipped-output decoder flush/retention | `tool.go`, `tool_output.go`; `tool_output_skip_test.go` and output/lifecycle suites |
| M `packages/durable/test/harness-view.test.ts` | Copied settings/progress | `settings.go`, `harness.go`, progress helpers; `progress_104_test.go`, settings/registry tests |
| A `packages/durable/test/provider-session-cache-e2e.test.ts` | Persisted UUIDv7 identity/fork/compaction | `provider_identity.go`, `generation.go`, `compaction.go`; identity and existing fork/compaction tests |
| A `packages/durable/test/tools-read-differential.test.ts` | Bounded read/full-file totals | `read_binary.go`, `binary_reader.go`, `truncate.go`; read reference/bounded/240 scanner cases; integral native args |
| M `packages/durable/test/tools.test.ts` | Bash/PowerShell direct argv | `bash_portable_unix.go`, `powershell_unix.go`; shell/PowerShell/Bash tests; live PowerShell/Windows unverified |
