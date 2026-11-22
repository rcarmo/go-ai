# Fetch-option originals: offline receipt

The whole unchanged `packages/ai/test/fetch-option.test.ts` passed: **6 passed, 0 failed, 0 skipped, 0 todo**. The [receipt](fetch-option.json) records the six exact reported names, test and provider-source digests, and the [prior queue row](primary-queue-reconciled.jsonl). The earlier aggregate receipt included this file and the Google continuation file; this new run establishes the fetch-option file's count separately, not another six cases to add to that aggregate.

The originals exercise request-local fetch for Anthropic, OpenAI and Azure SDK adapters, Mistral, Codex SSE, pi-messages, Google and Vertex, and OpenRouter image generation. They also test Google adapter calls without a custom fetch: the ambient stub receives two calls without an SDK-supplied dispatcher. The tests use dummy keys, stubbed responses, and controlled fetch call counts; this is not live provider or timeout evidence.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-fetch-option.sh" "$pinned_git_mirror" "$retained_oracle" \
  > /tmp/pi-v160-fetch-option.json
cmp "$inventory/fetch-option.json" /tmp/pi-v160-fetch-option.json
```

The probe stages source from `faac585884ae2c013a6b1400f3e9e77f10b34bac` and copies the selected installed dependency closure to disposable scratch. It checks the exact source bytes before and after the run, including the unchanged runner configuration, and checks the six names from the Vitest report. It runs under the shared lock with a scrubbed environment and Node's permission model without network permission. No installs or provider credentials were used.

This run used Node `v26.13.0` and Vitest `4.1.12` on Linux arm64/glibc. There were no credential or platform gates in this selection. Live provider credentials, other hosts, transitive installed-byte verification and exact native assertion mappings remain separate work. No exclusions or release acceptance are implied.
