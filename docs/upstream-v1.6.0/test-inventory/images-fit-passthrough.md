# Durable fitting leaves unchanged content alone

The [Oracle receipt](images-fit-passthrough.json) records one pass, no failures or selected skips, and ten filtered cases from unchanged pi v1.6.0 `packages/durable/test/images-fit.test.ts:88–97`. The [resize receipt](images-fit-resize.json) covers a different original.

The pinned Photon runtime witness observed one PNG request across both calls in the original. It matched the loaded JavaScript and instantiated WASM to the verified archive. The worker returned the same PNG byte count and digest as the input, with neither resize nor conversion metadata. Text-only content made no second request. Changing the initial `changed` flag to `true` fails the unchanged original at line 91 but leaves the input and worker trace identical.

## Native comparison

[`TestSourceV160FitPassthrough`](../../../durable/tools/image_fit_passthrough_upstream_test.go) passes the captured 8×6 PNG through public `FitImages`. The recording callback delegates to the real Go `NativeImageProcessor`.

| Source assertion | Native comparison |
| --- | --- |
| Line 91 | Mixed text and an in-limit PNG return the zero `FittedImages` value: unchanged, no replacement content, no notes. |
| Line 92 | One call receives the exact PNG bytes and MIME `image/png`, under the default width, height and base64 limits. |
| Lines 93–96 | Text-only input also returns unchanged and makes no additional processor call. |

The test also checks that fitting leaves the caller's content untouched. The compiled baseline passed with no production change. Forcing Go to report a change failed both unchanged-result checks without changing the processor call record. Restored focused tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, and ShellCheck passed. The receipt and the captured PNG reproduced byte-for-byte.

This is a pass-through comparison, not a claim that different encoders produce identical bytes or that Go matches Photon worker scheduling.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp)
trap 'rm -f -- "$scratch"' EXIT
bash "$inventory/../dependencies/probe-photon-runtime.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  fit-passthrough > "$scratch"
cmp "$inventory/images-fit-passthrough.json" "$scratch"
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run TestSourceV160FitPassthrough -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle runner takes the shared lock itself.
