# Durable fitted image order and coordinate note

The [Oracle receipt](images-fit-resize.json) records one pass, no failures or selected skips, and ten filtered cases. It reruns unchanged pi v1.6.0 `packages/durable/test/images-fit.test.ts:99–114`, previously selected in the [Photon runtime receipt](../dependencies/photon-runtime.json).

The Photon witness captures the exact input PNG from the real worker request and verifies the loaded JavaScript and instantiated WASM against the pinned npm archive. The worker reported a PNG resized from 400×100 to 200×50 with `maxWidth: 200`, `maxHeight: 2000` and a 4,718,592-byte base64 cap. The runtime uses retained dependencies and runs without Node network permission.

Changing only the coordinate multiplier format from two decimal places to one fails the original at line 111. The captured input and observed worker trace are identical: the fault changes the note, not the image operation. Both the receipt and the input PNG reproduced byte-for-byte.

## Native comparison

[`TestSourceV160FitResizeOrderAndNote`](../../../durable/tools/image_fit_resize_upstream_test.go) passes the exact captured PNG to public Go `FitImages` with `NativeImageProcessor`. It checks:

- The result is fitted (line 106) and contains text, image, text in order (line 108).
- The surrounding text values are unchanged. The JavaScript reference-identity check at line 109 has no direct Go `ContentBlock` value equivalent.
- Decoding the resulting image gives exactly 200×50 pixels (line 110).
- The sole note is `Resized from 400x100 to 200x50. Multiply coordinates by 2.00 to map them to the original.` (lines 111–113).

The Go test also checks the caller's input, the output MIME and byte cap. The compiled baseline passed without a production change. Formatting the Go multiplier as `2.0` failed only the exact note assertion. After restoring the baseline, focused tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, and ShellCheck passed.

These comparisons do not equate the encoders' output bytes or establish worker scheduling parity.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp)
trap 'rm -f -- "$scratch"' EXIT
bash "$inventory/../dependencies/probe-photon-runtime.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  fit-resize > "$scratch"
cmp "$inventory/images-fit-resize.json" "$scratch"
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run TestSourceV160FitResizeOrderAndNote -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle runner takes the shared lock itself.
