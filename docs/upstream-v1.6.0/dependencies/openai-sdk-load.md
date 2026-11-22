# OpenAI SDK loaded input: one pinned Oracle case

Node's load hook returned `node_modules/openai/index.mjs` as ESM in a new focused run. Its **787 bytes** matched `package/index.mjs` in the [verified OpenAI `7.59.0` archive](provider-archives.md). The [receipt](openai-sdk-load.json) records the exact pin, entry path, archive integrity and loaded digest. This is the package entry point, not a trace of its imports.

The run selected **keeps reasoning.effort constant and only appends input when the level changes** from the unchanged `packages/ai/test/openai-responses-mid-convo-effort.test.ts`. The test uses a dummy key and stub fetch to return SSE events; it checks the fixed request effort and exact in-conversation update sequence. One selected original passed on Node `v26.13.0` and Vitest `4.1.12`. The other cases were outside the selector, not approved exclusions.

Appending one LF to the copied SDK entry still let the same unchanged test pass. The load hook returned the changed 788-byte input and the archive-digest check rejected it. The [Google run receipt](google-sdk-load.md) is separate and unchanged.

## Reproduce offline

```sh
ledger=docs/upstream-v1.6.0/dependencies
bash "$ledger/probe-openai-sdk-load.sh" "$pinned_git_mirror" "$oracle" "$retained_archive_dir" \
  > /tmp/pi-v160-openai-sdk-load.json
cmp "$ledger/openai-sdk-load.json" /tmp/pi-v160-openai-sdk-load.json
```

The probe checks the compressed archive against the pinned lock, stages the selected source from Git, and copies only compatible, present npm placements reachable from the Vitest, OpenAI and `partial-json` roots along resolved runtime, optional and required-peer edges. It includes 46 npm placements on the observed Linux arm64/glibc host, plus the pinned Schema and Telemetry source trees. The copied list is not a load list or a byte check of every dependency.

Both focused runs use the shared lock, a scrubbed environment and Node's permission model without network permission. The probe writes only disposable scratch and prints one expected digest rejection on stderr. It makes no installs, shared-cache changes or credentialed calls. This adds one current-run loader observation; it does not reconstruct past inputs, validate all transitive loads, or extend native parity or acceptance.
