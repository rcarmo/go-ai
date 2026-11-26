# Responses replay keeps tool calls completed

At the pinned pi v1.6.1 commit `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`, the shared Responses converter marks replayed function and custom tool calls as `completed`. The new source test checks the entire objects, including their item IDs, call IDs, names and function arguments or custom input. Both Go serializers omitted only `status` on the unchanged native base `87cacdd38c7ac2a63069333d7a79cddcff5cdf39`.

## Pinned original

The unchanged `packages/ai/test/openai-responses-tool-call-replay.test.ts` passed once without skips. Nine selected source, catalog entry point and configuration files were byte-compared against the pinned Git tree. The test SHA-256 was `01ebedc24639091d695f41fd4c58ce20195fd5ca434b60092953f14dfdbb38fb`. The staged model data came from the verified `pi-ai-1.6.1.tgz` archive (SHA-256 `192c407bd69c06358c68519aa6290adc8362b132bee9117480c7604456d5073b`). The manifest and both `openai` and `openai-codex` shards were compared byte for byte with that archive. The original uses the Codex GPT-6.2 Sol model and calls the shared converter directly.

The run used Node 26.13.0 and Vitest 4.1.12 in a disposable copy, with a scrubbed environment, temporary home and isolated network namespace. From the staged `packages/ai` directory:

```sh
node ../../node_modules/vitest/vitest.mjs run test/openai-responses-tool-call-replay.test.ts \
  --pool=forks --maxWorkers=1 --reporter=json
```

## Native replay

`TestSourceV161ResponsesToolCallReplay/codex` checks the two ordered items in a public Go completion using the same published model. The `/responses` row covers the other Go serializer with the corresponding OpenAI model as a separate check. Both rows require one scripted POST, use synthetic authentication and decode the request body before comparison. The fixture selects the grammar input property `code` with a declared tool; the source converter accepts that property through its argument map.

The native fixture compiled and failed four exact comparisons before the fix. Four independent controls removed the status from one call type on one adapter; each failed only the corresponding comparison. The restored checks used Go 1.27.3, the project build cache and shared verification lock:

```sh
unshare -Un go test ./inference/provider/openairesponses \
  -run '^TestSourceV161ResponsesToolCallReplay$' -count=3 -shuffle=42 -p 1 -timeout 60s
go vet -p 1 ./inference/provider/openairesponses ./inference/provider/openaicodex
```

Both full provider suites passed on the unchanged base. After the fix, the full Responses suite passed twice and the full Codex suite passed on retry. An earlier Codex repeat hit `TestCodexIdleTimeoutWire/abort-after-idle` once: `[start aborted]` instead of `[aborted]`. Forty isolated attempts on unchanged production did not reproduce it. The Codex suite also passed twice with that timeout test excluded. Vet and Staticcheck 0.8.2 passed for both packages.

The tests cover full input replay. They do not test a real WebSocket idle timeout, server prefix validation or underlying live model behaviour.
