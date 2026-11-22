# Together model catalogue: exact offline slice

The [Oracle receipt](together-models.json) verifies two unchanged model-only cases in `packages/ai/test/together-models.test.ts` at pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`: Kimi K3's model record and the GPT-OSS/DeepSeek reasoning maps. Both passed on Node `v26.13.0` and Vitest `4.1.12`, with no skip among the selected cases. The dummy environment-key case was outside the selector, not an exclusion.

The run supplied all 43 provider JSON files and the schema-v6 manifest from the verified release archive, checking each raw digest against the [asset receipt](../dependencies/provider-catalog-assets.md). The Together member's SHA-256 is `d2d3a8dfbcfdf8295ba386f32f5e10665615eb4c1a8b51978e75372a9d487339`. In a disposable control, replacing Kimi K3's `maxTokens` with the old value `131072` failed precisely its unchanged assertion while the reasoning-map case still passed. No model data was regenerated or committed.

The [public native comparisons](../../../tests/models_catalog_upstream_test.go) keep the original API/provider, URL, reasoning, input, price, and shape comparisons. The pre-change native test failed because it expected the historical maximum `131072`; the current catalogue already exposes `1048576` for both context and output. The Kimi compatibility comparison now checks the exact struct: developer role is absent, and the four false flags are explicit. GPT-OSS and DeepSeek keep exact reasoning maps and the original compatibility subset; the earlier MiniMax check remains supplemental. An explicit developer-role `false` in the expected Kimi record fails the comparator because the record omits that field.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-together-models.sh" "$pinned_git_mirror" "$retained_oracle" "$release_archive_dir" \
  > /tmp/pi-v160-together-models.json
cmp "$inventory/together-models.json" /tmp/pi-v160-together-models.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./tests \
  TEST_FLAGS='-run "TestTogetherModels(RegistersDefaultKimiK3|ReasoningControls)" -count=2 -shuffle=42 -p 1 -timeout 65s'
```

The Oracle probe uses exact Git source, isolated scratch and a scrubbed environment with no Node network permission. The focused native tests passed twice on Go `1.27.3` with CGO disabled. Focused vet, Staticcheck `0.8.2` and diff checks passed. No real keys, installs, provider calls or release acceptance are part of this slice.
