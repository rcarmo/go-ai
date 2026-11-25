# Durable fitting converts BMP to PNG

The [Oracle receipt](images-fit-bmp.json) records one pass, no failures or selected skips, and ten filtered cases from unchanged pi v1.6.0 `packages/durable/test/images-fit.test.ts:128–132`. The case expects a BMP to become PNG with the sole note `Converted from image/bmp to image/png.`

The Photon witness captures the exact 4×4 BMP from the worker request and matches the loaded JavaScript and instantiated WASM to the pinned npm archive. The worker returns PNG with `convertedFrom: "image/bmp"` and no resize metadata. Removing only the note fails the unchanged original at line 131. The input and worker trace are identical in that control. The receipt and input BMP reproduced byte-for-byte.

## Native comparison

[`TestSourceV160FitBMPConversion`](../../../durable/tools/image_fit_bmp_upstream_test.go) calls public `FitImages` with `NativeImageProcessor` and the captured BMP. It checks one image with MIME `image/png` (line 130) and the exact sole conversion note (line 131). It also decodes the result as a 4×4 PNG under the configured base64 cap, and checks that the processor receives the exact input and default limits once without altering the caller's content.

The compiled baseline passed without a production change. Suppressing the conversion note failed only the native note assertion. Restored focused tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, and ShellCheck passed.

This is a format and diagnostic comparison, not a claim of identical encoded bytes, pixel fidelity, or worker scheduling.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp)
trap 'rm -f -- "$scratch"' EXIT
bash "$inventory/../dependencies/probe-photon-runtime.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  fit-bmp > "$scratch"
cmp "$inventory/images-fit-bmp.json" "$scratch"
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run TestSourceV160FitBMPConversion -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle runner takes the shared lock itself.
