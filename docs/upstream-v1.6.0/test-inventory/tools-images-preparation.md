# Durable image preparation and EXIF

The [Oracle receipt](tools-images-preparation.json) records **23 unchanged originals passing** from pi v1.6.0 `packages/durable/test/tools-images.test.ts`. The first run passed 22 cases covering prepared reads, JPEG/WebP EXIF, header dimensions, and base64. A separate run passed the worker/calling-thread equality case and its input-byte preservation assertion. It used an 1856 MiB Node old-space cap for the original's multi-megabyte typed-array comparison; the other run kept the 768 MiB cap. Neither selection had failures or selected skips.

The probe stages exact Git source, checks the retained Photon JavaScript and WASM against the verified archive, and verifies the pi-ai archive. It uses no live credentials or Node network permission. This receipt records preparation results and input provenance, not a new witness of the worker's runtime load. The same output can be produced by a fallback implementation; the equality test alone does not prove event-loop scheduling.

Together, this packet and the [nine read originals](tools-images.json) cover **32 distinct cases**. The new receipt lists all 11 unselected names: nine already covered by the first packet and these two still open, not excluded:

- `createNodePhotonImages prepares in a worker thread, leaving the event loop free`
- `createNodePhotonImages prepares concurrent images, and starts a new worker after the idle one exited`

Both involve host timing or delay assumptions. They were not selected for these runs.

## Native comparisons

[`TestSourceV160ImagePreparation`](../../../durable/tools/image_preparation_upstream_test.go) compares decoded dimensions and all six centre colours for each of the eight EXIF transforms through `PrepareImage`. Go encodes its own JPEG fixture and applies the same 127 channel threshold as upstream; exact codec bytes are not compared. The test also checks the exact JPEG and WebP orientation parser fixtures, including the odd preceding WebP chunk, and exercises the extreme aspect ratio, impossible byte budget, and JPEG output format. The impossible budget returns a Go error with no image rather than JavaScript `undefined`.

These 13 cases passed against the unmodified native implementation. The source BMP read case passed its output block comparison but failed the exact conversion message in Go: it returned only `Read image file [image/png]`. The [read result](../../../durable/tools/image.go) now includes `Converted from image/bmp to image/png.` when a preparation changed the format. No image codec or transformation changed.

In both source and Go, substituting the orientation 8 transform for orientation 7 failed only the orientation 7 comparison, on the centre colours. Both source receipts reproduced byte-for-byte. Focused Go tests passed twice with CGO disabled after restoration; vet, Staticcheck `0.8.2`, ShellCheck, and diff checks passed.

The native packet maps 14 cases from this selection. Exact diagnostics for prepared resize and corrupt input, the synthetic WebP header dimensions, and the Oracle's worker-specific behavior remain outside that mapping. The baseline evidence is not a comparison of codec internals or a platform matrix.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-tools-images.sh" "$pinned_git_mirror" "$retained_oracle" \
  "$HOME/.npm/_cacache" "$release_archive_dir" preparation > /tmp/pi-v160-tools-images-preparation.json
cmp "$inventory/tools-images-preparation.json" /tmp/pi-v160-tools-images-preparation.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run "TestSourceV160ImagePreparation|TestSourceV160InlineReadImageSafety|TestPinnedImageDetection|TestRead104|Test.*Image" -count=2 -shuffle=42 -p 1 -timeout 120s'
```

The Oracle probe takes the shared lock itself. Omitting `preparation` reproduces the earlier read receipt.
