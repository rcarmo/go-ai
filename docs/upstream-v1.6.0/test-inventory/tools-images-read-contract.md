# Durable inline image read contract

The [Oracle receipt](tools-images-read-contract.json) selects three unchanged cases from pi v1.6.0 `packages/durable/test/tools-images.test.ts`: **3 passed, 0 failed, 0 selected skips, 31 filtered**. Two were in the [inline read receipt](tools-images.json), and the third was in the [preparation receipt](tools-images-preparation.json). The union stays at **32 distinct originals of 34**.

The probe verifies the pinned test and helper source, the Photon JavaScript and WASM against the npm archive, and the pi-ai archive. It stages the retained dependencies offline and runs Node without credentials or network permission. A separate call to the verified fixture helper records the small input payloads used by the selected tests. This packet does not add a worker load witness.

Removing BMP from the scratch processor-enabled description failed only the assertion at line 168. Ignoring the supplied byte cap during preparation failed only the assertion at line 236. Both controls ran the original test unchanged. The receipt reproduced byte-for-byte.

## Exact Go comparisons

[`TestSourceV160ImageReadContract`](../../../durable/tools/image_read_contract_upstream_test.go) exercises the public Go tool registrations and processor. Its PNG and GIF fixtures are the exact encoded inputs stored in the receipt.

| Original assertions | Native comparison |
| --- | --- |
| Lines 114, 115, 117 | `Read` on a local filesystem returns a single full image block, with the original base64 and MIME, for both PNG and GIF. The PNG produces exactly `Read image file [image/png].` as its sole diagnostic message. |
| Lines 167–168 | The unconfigured `Read` description lists `(jpg, png, gif, webp)`. `ReadWithImages` and `ReadWithPreparedImages` list `(jpg, png, gif, webp, bmp)`. |
| Line 236 | The public `NativeImageProcessor` returns no image and no error for the same valid 4×4 PNG at an eight-byte base64 cap. The same fixture fits at the default cap. |

The compiled Go baseline passed byte identity and the eight-byte cap. It failed only the description case: `Read` described text files but omitted its image formats. The fix changes registration metadata only. Dropping a byte from the native inline payload and ignoring the native cap each failed only the corresponding subtest. Restored focused Go tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, and ShellCheck passed.

This packet does not change encoder choices or the resource behavior of Go or Photon workers. It does not cover the timing or idle-restart originals.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp)
trap 'rm -f -- "$scratch"' EXIT
bash "$inventory/probe-tools-images.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  read-contract > "$scratch"
cmp "$inventory/tools-images-read-contract.json" "$scratch"
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run TestSourceV160ImageReadContract -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle probe acquires the shared lock itself.
