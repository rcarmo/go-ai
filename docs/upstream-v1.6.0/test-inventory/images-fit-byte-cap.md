# Durable image fitting under an encoded byte cap

The [Oracle receipt](images-fit-byte-cap.json) covers unchanged pi v1.6.0 `packages/durable/test/images-fit.test.ts:116–121`: one pass, no failures or selected skips, and ten filtered cases. The original starts with a 64×64 PNG that fits the dimension limits but exceeds the **8,000-byte base64 cap**. It requires a fitted image at or under the cap (line 119) and at least one note (line 120).

The Photon witness records the exact input from the worker request and matches the loaded JavaScript and instantiated WASM to the pinned npm archive. Removing only the notes fails line 120 without changing the input or worker trace. The receipt and captured PNG reproduced byte-for-byte.

## Native comparison

[`TestSourceV160FitEncodedByteCap`](../../../durable/tools/image_fit_byte_cap_upstream_test.go) loads the captured input from the receipt and calls public `FitImages` with `NativeImageProcessor`. It verifies that the original is over budget, fits one decodable image under the same encoded cap, and checks that its note list is nonempty. It also checks MIME against the decoded result, dimension limits, and caller ownership.

The captured input was 22,024 base64 bytes. Photon returned 7,092 and Go returned 4,404. Both returned JPEG and reported a note. Go's output decoded to 64×64; the Photon reply carried no resize metadata. The compiled native baseline passed without a production change. Suppressing the notes failed the Go note assertion while the cap still held. Restored focused tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, and ShellCheck passed.

Neither the original nor this comparison requires a particular output MIME, identical encoder bytes, or matching worker scheduling.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp)
trap 'rm -f -- "$scratch"' EXIT
bash "$inventory/../dependencies/probe-photon-runtime.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  fit-byte-cap > "$scratch"
cmp "$inventory/images-fit-byte-cap.json" "$scratch"
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run TestSourceV160FitEncodedByteCap -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle runner takes the shared lock itself.
