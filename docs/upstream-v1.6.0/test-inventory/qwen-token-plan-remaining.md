# Qwen Token Plan auth, reasoning levels, and request payloads

The [Oracle receipt](qwen-token-plan-remaining.json) records **110 passes, zero failures, and zero selected skips** from the unchanged pi v1.6.0 `packages/ai/test/qwen-token-plan-models.test.ts`. Its ten filtered names match the earlier [catalogue selection](qwen-token-plan-models.json) exactly. The two disjoint receipts cover **all 120 originals in this file**, with no exclusions. They are separate filtered runs, not an unfiltered run or a live provider check.

The probe verifies the pinned test and relevant module bytes, the published archive and all 44 catalogue assets, and the identities of 46 retained dependency placements. It reuses the Fireworks loopback-only Linux namespace and additionally denies Node network permission for the Qwen tests. The source supplies dummy keys and mocks the OpenAI SDK; this is not a witness that the real SDK was loaded. Changing only Individual GLM 5.3's `low` mapping to `medium` in scratch failed exactly the two unchanged GLM 5.3 map assertions at lines 243 and 259. All 108 other selected cases passed. The probe restores and rechecks the assets after the fault.

## Native comparisons

[`tests/qwen_token_plan_upstream_test.go`](../../../tests/qwen_token_plan_upstream_test.go) covers all four plans. It compares present `nil` entries separately from missing thinking-map keys and checks exact scalar values or omission in `OnPayload`. The production Completions stream runs to completion through a fake SSE request transport. The Go fixture uses a loopback-only fallback URL; it neither calls a provider endpoint nor proves that it is reachable.

| Pinned lines | Originals | Native evidence |
| --- | ---: | --- |
| 160–181 | 4 | Scoped key lookup and request key resolution: plan-specific precedence and regional shared fallback. |
| 183–186 | 1 | Partial: neither tested cross-plan credential resolves. Go does not expose the `findEnvKeys` discovery API. |
| 203–234 | 52 | `enable_thinking: true`; no `thinking` property. |
| 236–285 | 27 | Exact values and nil entries for the six asserted map keys. The separate Individual GLM 5.3 original repeats one table row's comparison and shares its native case. |
| 300–365 | 26 | `reasoning_effort: "high"` where requested; Qwen 3.8 `xhigh` with thinking enabled and no `thinking` property. |

Native reasoning and mock-stream comparisons passed before the runtime change. Compiled native tests failed on key order for both Team regions and global Individual, and on the missing China Individual shared fallback. The [Go credential table](../../../env.go) now tries the exact plan-specific key before the corresponding shared regional key. Reversing just the global Team order caused exactly that native subtest to fail. No generated catalogue or provider serializer changed.

## Reproduce offline

The Oracle probe requires Linux user/network namespaces, `ip`, and `setpriv`; it acquires the shared lock itself. Both Qwen receipts and both Fireworks namespace receipts reproduced byte-for-byte after sharing the namespace helper. Focused Go tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, ShellCheck, and diff checks passed.

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-qwen-token-plan-models.sh" "$pinned_git_mirror" "$retained_oracle" "$release_archive_dir" remaining \
  > /tmp/pi-v160-qwen-token-plan-remaining.json
cmp "$inventory/qwen-token-plan-remaining.json" /tmp/pi-v160-qwen-token-plan-remaining.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./tests \
  TEST_FLAGS='-run "QwenTokenPlan|TestEnvAPIKeys|TestFireworksProviderEnvKeyCompatibilityRetained" -count=2 -shuffle=42 -p 1 -timeout 65s'
```
