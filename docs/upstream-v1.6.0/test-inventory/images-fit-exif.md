# Durable fitting applies EXIF before measuring image size

The [Oracle receipt](images-fit-exif.json) records one pass, no failures or selected skips, and ten filtered cases from unchanged pi v1.6.0 `packages/durable/test/images-fit.test.ts:123–126`. The assertion decodes an in-limit JPEG with EXIF orientation 6 after fitting and checks for **30×40** pixels.

The Photon witness captures the exact input from the worker request and matches the loaded JavaScript and instantiated WASM to the pinned npm archive. The worker returns a re-encoded JPEG under the default limits, with no resize or conversion metadata. Applying orientation 3 instead leaves the input and loaded assets unchanged but changes the reply and fails the dimension assertion at line 125. The receipt and input JPEG reproduced byte-for-byte.

## Native comparison

[`TestSourceV160FitEXIFOrientation`](../../../durable/tools/image_fit_exif_upstream_test.go) passes the exact captured JPEG through public `FitImages` and `NativeImageProcessor`. It verifies a stored 40×30 input and decodes the output as 30×40, within the base64 budget. Applying orientation alone emits no resize or conversion note and does not mutate the caller's input.

The compiled baseline passed without a production change. Forcing orientation 3 in Go produced 40×30 and failed only the dimension assertion. Restored focused tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, and ShellCheck passed.

The exact assertion is about decoded dimensions, not pixel placement, encoder fidelity, or worker scheduling.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp)
trap 'rm -f -- "$scratch"' EXIT
bash "$inventory/../dependencies/probe-photon-runtime.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  fit-exif > "$scratch"
cmp "$inventory/images-fit-exif.json" "$scratch"
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run TestSourceV160FitEXIFOrientation -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle runner takes the shared lock itself.
