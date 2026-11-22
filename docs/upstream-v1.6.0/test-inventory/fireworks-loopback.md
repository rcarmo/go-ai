# Fireworks Messages: offline session headers and tool fields

The [Oracle receipt](fireworks-loopback.json) records **10 passes, 0 failures, and 0 selected skips** from the unchanged `Anthropic-compatible session affinity and tool compat` suite in pi v1.6.0 `packages/ai/test/fireworks-models.test.ts`. The probe checks 43 published provider files and their schema-v6 manifest against the [asset receipt](../dependencies/provider-catalog-assets.md). The JSON names all ten selected cases and all 15 filtered cases. Three of the latter have an [earlier catalogue receipt](fireworks-models.md); the other 12 remain open in these two packets. No case was excluded.

The original helper listens on `127.0.0.1`, directs each model at that server, and captures the SDK's actual HTTP request. Each run uses an unprivileged Linux user/network namespace containing only `lo`. The probe checks both IP route tables and drops capabilities before Node runs. An IPv4 TCP round trip succeeds inside the namespace; attempts to connect to IPv4 and IPv6 documentation addresses return `ENETUNREACH`. Node receives a scrubbed environment, and the unchanged tests use only dummy credentials. This is IP network isolation, not a claim about arbitrary non-IP interprocess communication. No dependency install or real provider request is needed.

Changing only the scratch provider's `x-session-affinity` header name caused the unchanged Fireworks test at line 343 to fail on the captured header. The other nine cases passed.

## Native comparison

[`TestSourceV160FireworksMessagesWire`](../../../tests/fireworks_models_v160_test.go) uses `goai.Stream` and captures requests on an ephemeral loopback HTTP server. It preserves the upstream fixture model metadata and tool schema; the Go adapter normalizes the URL to `/v1/messages`. It verifies the presence and exact value, or absence, of the relevant headers and the original tool field comparisons. Empty-session and model/caller header merge tests are supplemental, not additional originals.

| Pinned lines | Native comparisons | Before the fix |
| --- | --- | --- |
| 336, 346, 355 | Fireworks affinity header, Anthropic omission, Fireworks cache-off omission | Fireworks affinity header missing; both omissions passed |
| 366, 375, 385 | OpenRouter session ID only, cache-off omission, explicit opt-out | OpenRouter session ID missing; both omissions passed |
| 394, 403, 413, 423 | Tool cache marker and eager streaming absent for Fireworks, present for native Anthropic | All four passed |

The [native adapter](../../../inference/provider/anthropic/anthropic.go) now derives the header only for a nonempty session with caching enabled. Fireworks opts in via model compatibility, OpenRouter defaults to `x-session-id`, and an explicit false still disables the derived header. Model and caller headers, including caller suppression, take precedence. Removing the new call reproduced exactly the two missing-header failures. No tool implementation or catalogue changed. The upstream custom `sessionAffinityFormat` option is not exercised by this source slice; this packet only covers the default formats selected by the tests.

The Oracle receipt reproduced byte-for-byte. The focused Fireworks comparisons and the Anthropic provider package each passed twice with Go `1.27.3` and CGO disabled. Vet and Staticcheck `0.8.2` passed for both Go packages; ShellCheck passed for the probe.

## Reproduce

The Oracle probe requires Linux with unprivileged user and network namespaces, `ip`, `setpriv`, and the same retained inputs as the [catalogue probe](fireworks-models.md). It acquires the shared verification lock itself.

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-fireworks-loopback.sh" "$pinned_git_mirror" "$retained_oracle" "$release_archive_dir" \
  > /tmp/pi-v160-fireworks-loopback.json
cmp "$inventory/fireworks-loopback.json" /tmp/pi-v160-fireworks-loopback.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./tests \
  TEST_FLAGS='-run TestSourceV160Fireworks -count=2 -shuffle=42 -p 1 -timeout 65s'
```
