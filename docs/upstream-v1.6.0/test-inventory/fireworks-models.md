# Fireworks model catalogue: offline slice

The [Oracle receipt](fireworks-models.json) records **three passes and zero skips among the selected cases** from `packages/ai/test/fireworks-models.test.ts` at pi v1.6.0 `faac585884ae2c013a6b1400f3e9e77f10b34bac`: the DeepSeek Flash Messages route, GLM Fast's alignment with GLM 5.3, and Kimi K3's compatibility and reasoning controls. The run used Node `v26.13.0` and Vitest `4.1.12`. The probe stages all 43 provider JSON files and their manifest from the verified release archive and checks each against the [asset receipt](../dependencies/provider-catalog-assets.md). The Fireworks member's SHA-256 is `71811ef203ad9e7284b6aa6647dedc665847a96a793cb8f364967256e3b6c647`.

The [public native comparisons](../../../tests/fireworks_models_v160_test.go) check those routes, exact compatibility and thinking maps, and `reasoning_effort: "max"` captured before any request through the public Kimi adapter. An unreachable-by-design loopback handler catches a missed payload hook without risking a real request. The exact Kimi record requires that `supportsDeveloperRole` be absent rather than explicit `false`. Reintroducing it in the scratch JSON failed only the unchanged Kimi compatibility assertion. Adding the field to the expected Go struct also failed the native comparator. No generated files or production code changed.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-fireworks-models.sh" "$pinned_git_mirror" "$retained_oracle" "$release_archive_dir" \
  > /tmp/pi-v160-fireworks-models.json
cmp "$inventory/fireworks-models.json" /tmp/pi-v160-fireworks-models.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./tests \
  TEST_FLAGS='-run "TestSourceV160FireworksModelProjection" -count=2 -shuffle=42 -p 1 -timeout 65s'
```

The probe acquires the shared lock itself. It uses exact Git source, isolated scratch, and a scrubbed Node environment without network permission. The receipt lists the 22 cases outside the selector, including dummy environment-key and loopback session/transport checks. None is an exclusion, and no real credential or provider network call is needed for this slice. Other hosts and live behavior remain open. The selected native tests passed twice with Go `1.27.3` and CGO disabled; focused vet, Staticcheck `0.8.2` and diff checks passed.
