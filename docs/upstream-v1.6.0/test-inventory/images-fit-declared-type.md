# Durable fitting corrects a declared image type

The [Oracle receipt](images-fit-declared-type.json) records one pass, no failures or selected skips, and ten filtered cases from unchanged pi v1.6.0 `packages/durable/test/images-fit.test.ts:134–137`. The case supplies JPEG bytes declared as PNG and expects the exact original bytes, labelled JPEG, without a note.

The Photon witness observed the real worker receive the JPEG MIME and bytes at the default limits. It matched the loaded JavaScript and instantiated WASM to the verified npm archive. The reply has the same byte count and SHA-256 as the input and carries no resize or conversion metadata. Changing only the output block's label to GIF fails line 136, with the input PNG declaration, JPEG bytes, and worker trace unaffected. The receipt and input JPEG reproduced byte-for-byte.

## Native comparison

[`TestSourceV160FitDeclaredImageType`](../../../durable/tools/image_fit_declared_type_upstream_test.go) uses public `FitImages` and the exact JPEG from the pinned worker request. The callback delegates to `NativeImageProcessor` and records one call with the JPEG MIME, exact bytes, and default limits. The fitted result changes only the MIME to `image/jpeg`: it preserves the original base64, adds no notes, and leaves the caller's content unchanged.

The compiled Go baseline passed without a production change. Labelling only the fitted block GIF failed the exact output check. Restored focused tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, and ShellCheck passed.

This compares a MIME correction without re-encoding. It does not claim byte fidelity across different encoders or worker timing.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp)
trap 'rm -f -- "$scratch"' EXIT
bash "$inventory/../dependencies/probe-photon-runtime.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  fit-declared-type > "$scratch"
cmp "$inventory/images-fit-declared-type.json" "$scratch"
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run TestSourceV160FitDeclaredImageType -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle runner takes the shared lock itself.
