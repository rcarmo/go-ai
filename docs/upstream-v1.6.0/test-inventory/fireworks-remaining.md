# Fireworks model and payload originals: remaining cases

The [Oracle receipt](fireworks-remaining.json) records **12 passes, zero failures, and zero selected skips** from the unchanged pi v1.6.0 `packages/ai/test/fireworks-models.test.ts`. It selects exactly the cases left after the [catalogue](fireworks-models.json) and [loopback](fireworks-loopback.json) packets. The 13 names filtered from this run exactly match those earlier selections. The three receipts cover **25 distinct originals from this file**, with no exclusions. They are separate runs, not a single unfiltered run or proof of live provider behavior.

The probe uses the same isolated Linux network namespace and scrubbed environment as the [loopback packet](fireworks-loopback.md). It compares the pinned test and selected provider/runner source bytes, verifies all 43 published provider files plus the schema-v6 manifest, and copies the same 54 retained dependency placements. Dummy keys do not authorize or require external traffic. Setting `forceAdaptiveThinking` to `false` on DeepSeek Flash in the scratch catalogue failed only its unchanged assertion at line 126; the other 11 selected cases still passed. The original catalogue was restored and rechecked.

## Native comparisons

[`TestSourceV160FireworksRemainingModels`](../../../tests/fireworks_models_v160_test.go) passes against the unmodified runtime. It uses the registered production `StreamSimple` adapters for request payloads and aborts in `OnPayload` before transport, as the originals do. A rejecting transport records any accidental HTTP attempt. JSON field absence is distinct from `null`, and thinking and effort objects are compared in full.

| Pinned lines | Cases | Native evidence |
| --- | ---: | --- |
| 43–66 | 2 | Both GLM routes omit `prompt_cache_retention` with long caching requested. |
| 120–147 | 3 | Adaptive Messages metadata, ordered levels and all 12 level-specific payloads, including disabled thinking and omitted `output_config` for `off`. |
| 150–157 | 4 | Exact ordered `[low, high, max]` on GLM, Kimi and their fast aliases. |
| 159–180 | 1 | Exact non-adaptive thinking payload with budget `16384`; no `output_config`. |
| 182–187 | 1 | [Partial](../../../tests/models_catalog_upstream_test.go): `GetEnvAPIKey` resolves the pinned dummy process key. Go has no public `findEnvKeys` list API, so the exact discovery-list assertion remains unmapped. |
| 189–198 | 1 | DeepSeek's five compatibility properties are checked as explicitly present booleans. |

A one-token increase in the scratch native budget serializer failed exactly the non-adaptive comparator. The mutation was removed. No production or generated files changed. Custom Messages `sessionAffinityFormat` and live provider behavior remain outside these tests.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-fireworks-loopback.sh" "$pinned_git_mirror" "$retained_oracle" "$release_archive_dir" remaining \
  > /tmp/pi-v160-fireworks-remaining.json
cmp "$inventory/fireworks-remaining.json" /tmp/pi-v160-fireworks-remaining.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./tests \
  TEST_FLAGS='-run "TestSourceV160Fireworks|TestFireworksProviderEnvKeyCompatibilityRetained" -count=2 -shuffle=42 -p 1 -timeout 65s'
```

The probe acquires the shared lock itself. The `remaining` and original loopback modes reproduced both receipts byte-for-byte. The focused native tests passed twice on Go `1.27.3` with CGO disabled; vet, Staticcheck `0.8.2`, ShellCheck and diff checks passed.
