# Durable reads of images that change

The [Oracle receipt](tools-images-changing-read.json) records two passes, zero failures or selected skips, and 32 filtered cases. It selects unchanged pi v1.6.0 `packages/durable/test/tools-images.test.ts:173–194`: one retry after growth, and rejection after a second change. It verifies the pinned source, the Photon archive members, and the pi-ai archive. The probe uses retained dependencies with Node's file permission scoped to scratch and no network permission. Both cases were already in the [inline read receipt](tools-images.json); original coverage remains **32 of 34**.

The source control lets a growing image take the same early return as growing text. It fails both originals at lines 183 and 193, while leaving the test file unchanged. The receipt reproduced byte-for-byte.

## Native mapping

[`TestSourceV160ChangingImageRead`](../../../durable/tools/image_growth_upstream_test.go) calls the public `Read` registration. A thin wrapper records positional requests, lets the actual local reader return bytes, then appends through the real local filesystem at the same point as the source fixture. It uses the exact 8×6 PNG input recorded with the [pinned fixture helper](tools-images-read-contract.json). No timer or synthetic file metadata is involved.

| Assertion | Public Go observation |
| --- | --- |
| Line 183: whole-file reads use the old then new length | The two offset-zero payload requests are exactly `len(png)` and `len(png) + 16`. |
| Line 185: returned bytes include the append | The single returned image contains the original PNG and all 16 appended zero bytes. |
| Line 193: a second change rejects the read | A file that gains a byte on each full read returns `busy.png changed while it was read`, with no image. |

The Go comparisons also check the actual bytes on disk, exactly one open and close, and two full read attempts in both cases.

The compiled baseline issued 512-byte signature probes instead of 32-byte probes. On the 137-byte fixture those probes were additional whole-file reads: the growing case requested `[512, 137, 512, 153]` instead of `[137, 153]`, and the busy file was appended to four times instead of twice. The fix matches the pinned 32-byte MIME probe. Retry rules, text scanning, and registration and schema are unchanged.

Letting Go accept growth for images failed both cases: it returned the pre-append bytes and accepted the busy file. After restoration, focused tests passed twice with CGO disabled. Vet, Staticcheck `0.8.2`, and ShellCheck also passed.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp)
trap 'rm -f -- "$scratch"' EXIT
bash "$inventory/probe-tools-images.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  changing-read > "$scratch"
cmp "$inventory/tools-images-changing-read.json" "$scratch"
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run TestSourceV160ChangingImageRead -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle runner acquires the shared lock itself. Neither the native comparison nor this source packet establishes worker timing or cross-runtime encoder fidelity.
