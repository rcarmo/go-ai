# Durable image omission and diagnostic notes

The [Oracle receipt](images-fit-omissions.json) records one pass, no failures or selected skips, and ten filtered cases from unchanged pi v1.6.0 `packages/durable/test/images-fit.test.ts:139–152`. This is distinct from the [resize](images-fit-resize.json) and [pass-through](images-fit-passthrough.json) originals.

The test tries a PNG truncated to 40 bytes, plain text declared as PNG, and malformed base64. The Photon witness verified the loaded JavaScript and instantiated WASM against the pinned npm archive. It saw two replies with no prepared image; malformed base64 never reached the worker. The receipt includes the full 4×4 PNG from the [pinned fixture helper](tools-images-read-contract.json), before truncation. The probe checks that both packets use the same pinned fixture helper bytes. This mode records replies, not the worker's input buffers.

Removing the base64 decoding error from the note fails the unchanged original at line 149. The worker trace stays identical, so the failure is confined to the note. The receipt reproduced byte-for-byte.

## Native comparison

[`TestSourceV160FitImageOmissions`](../../../durable/tools/image_fit_omissions_upstream_test.go) exercises public `FitImages` and delegates admitted payloads to `NativeImageProcessor`.

| Source assertion | Go comparison |
| --- | --- |
| Line 144 | The truncated PNG is omitted, the original text block remains, and the sole omission note is exact. |
| Line 146 | The non-image bytes produce no image and the same exact note. |
| Lines 148–151 | Malformed base64 produces no image and a note beginning `An image (image/png) cannot be prepared for the model and was left out: `. The original does not pin the decoder's explanation after the colon. |

Only the first two inputs reach the Go processor. It receives the pinned bytes, MIME and default limits unchanged. The caller's content is not mutated. The compiled baseline passed without a production change. Dropping the decoder error in Go failed only the malformed base64 note check. Restored focused tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, and ShellCheck passed.

This packet does not claim worker scheduling or encoder fidelity.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp)
trap 'rm -f -- "$scratch"' EXIT
bash "$inventory/../dependencies/probe-photon-runtime.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  fit-omissions > "$scratch"
cmp "$inventory/images-fit-omissions.json" "$scratch"
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run TestSourceV160FitImageOmissions -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle runner takes the shared lock itself.
