# Qwen Token Plan catalogue membership

The [offline Oracle receipt](qwen-token-plan-models.json) records **10 passes and zero skips among selected unchanged cases** from pi v1.6.0 `packages/ai/test/qwen-token-plan-models.test.ts`. It lists all ten selected names and the 110 reported cases outside the selector. The selection checks exact Individual model lists for both endpoints, required Team text models, omitted Team image IDs, and retired IDs absent under all four plans. Other cases, including dummy-key auth and payload variants, remain open and are not exclusions.

The probe stages 43 provider files and the schema-v6 manifest from the verified pi-ai release archive, checks each byte digest against the [asset receipt](../dependencies/provider-catalog-assets.md), and records all four Qwen data digests. Adding retired `deepseek-v4-flash` to the scratch Team catalogue failed precisely its unchanged retirement assertion; the other nine selected cases still passed. No retained or generated catalogue was changed.

The [native registry comparisons](../../../tests/qwen_token_plan_upstream_test.go) use `ListModels`. Before the change the Team fixture failed because it still expected retired `deepseek-v3.2`. The new tests compare exact sorted Individual IDs and preserve the older supplemental API, provider and dated Flash checks. The two Individual endpoints come from the published records; this is a metadata comparison, not endpoint reachability. Swapping `qwen4-max-preview` with an obsolete ID in the expected list fails the native comparator without changing the count. No runtime changes or model regeneration were needed.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-qwen-token-plan-models.sh" "$pinned_git_mirror" "$retained_oracle" "$release_archive_dir" \
  > /tmp/pi-v160-qwen-token-plan-models.json
cmp "$inventory/qwen-token-plan-models.json" /tmp/pi-v160-qwen-token-plan-models.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./tests \
  TEST_FLAGS='-run "TestQwenTokenPlanModelsExposeTextAndOmitImageModels" -count=2 -shuffle=42 -p 1 -timeout 65s'
```

The Oracle probe takes the shared lock itself. It copies only the selected local dependency closure and uses a scrubbed Node environment without network permission. The observed host was Linux arm64/glibc; no credentials, installs or network requests were used. The focused native run passed twice on Go `1.27.3` with CGO disabled; vet, Staticcheck `0.8.2` and diff checks passed.

The remaining [Fireworks originals](fireworks-models.md) cannot all run with this exact network policy: `captureAnthropicRequest` in pinned `fireworks-models.test.ts` listens on IPv4 loopback and points the model at that local server. The next offline packet can use a loopback-only sandbox for those cases; broad network access is not needed. No remaining Fireworks result is claimed here.
