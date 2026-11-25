# Durable model width limits for image reads

The [Oracle receipt](tools-images-model-limits.json) covers the unchanged pi v1.6.0 `packages/durable/test/tools-images.test.ts:158–164` original: **one pass, no failures or selected skips, 33 filtered**. The probe verifies pinned source and the Photon and pi-ai archives, then runs Vitest with retained dependencies and no Node network permission. This original already appears in the [inline read receipt](tools-images.json); the suite's original coverage remains **32 of 34**.

Ignoring the configured width and using the default instead fails the unchanged source at line 161. The receipt records the exact 8×6 PNG used in the [pinned fixture evidence](tools-images-read-contract.json) and reproduced byte-for-byte.

## Public Go comparison

[`TestSourceV160ModelImageLimits`](../../../durable/tools/image_model_limits_upstream_test.go) writes that exact PNG to a local file and invokes both public `read` registrations through Durable. A local provider fixture emits the tool call but does not construct or modify its invocation API. Durable resolves the conversation model from its catalogue, with `MaxWidth: 4` as the only image-limit override.

| Source assertion | Go comparison |
| --- | --- |
| Line 161: refusal names `8x6` against `4x2000` | Without preparation, the tool returns no image and one diagnostic containing `of 8x6, larger than 4x2000,`. The unset height stays at its default. |
| Line 163: decoded output is `4×3` | With preparation, the tool returns one image. Decoding its base64 payload gives exactly `4×3` pixels. |

Both registrations also return the original PNG bytes unchanged at default limits. The compiled Go baseline passed without a production change. Forcing the default width in Go failed both assertions: the unprepared read returned an image, and the prepared image remained 8×6. After restoring the original implementation, focused tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, and ShellCheck passed.

The comparison does not require encoded bytes or pixel values from different encoders to match. It does not cover the neighbouring text-only model details case, Photon worker timing, or other model limit fields.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp)
trap 'rm -f -- "$scratch"' EXIT
bash "$inventory/probe-tools-images.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  model-limits > "$scratch"
cmp "$inventory/tools-images-model-limits.json" "$scratch"
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run TestSourceV160ModelImageLimits -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle runner takes the shared lock itself.
