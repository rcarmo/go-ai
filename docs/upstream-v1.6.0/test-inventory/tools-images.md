# Durable image reads: offline safety checks

The [Oracle receipt](tools-images.json) records **nine passes, zero failures and zero selected skips** from the unchanged pi v1.6.0 `packages/durable/test/tools-images.test.ts`. It selects the read-without-processor and changing-file groups, including the model-width test's processor branch. It lists all 25 other cases by name; none is excluded. The narrower run followed a wider attempt that exhausted the 768 MiB Node heap before producing a usable report. That attempt does not establish a result for any additional case.

The probe verifies the test against the [Round 21 queue](primary-queue-r21.jsonl), stages exact Git files, and verifies the compressed Photon and pi-ai archives. Both installed Photon members match the archived JavaScript and WASM bytes. This is an input comparison, not a new trace of worker loads. Node runs without network permission in a scrubbed environment. Removing the scratch inline size guard changed the oversized-image result to an image and failed only the unchanged assertion at line 123.

## Native comparisons

[`TestSourceV160InlineReadImageSafety`](../../../durable/tools/read_image_limits_upstream_test.go) invokes the real `Read` registration with local files and a counted positional-reader wrapper. It checks the three source cases at lines 120, 129 and 139: byte-limit refusal before a full read, width/height refusal for PNG and GIF, and BMP conversion refusal without a processor. The Go PNG fixture uses no compression to reproduce the same over-budget dimensions without needing Photon. It stays below the native raw-byte safety cap, so it distinguishes the base64 limit from that cap.

The compiled baseline read **3,698,813 bytes** for the oversized fixture, in requests as large as **1,029,437 bytes**. All four fixture diagnostics also differed. The [positional path](../../../durable/tools/read_binary.go) now resolves the model budget before allocating the payload, checks encoded length without expanding the image to base64, and uses the same guarded path for the ordinary local environment. Rejections keep the source path, MIME type, and relevant dimensions or size. Host safety limits and the prepared-image path remain unchanged.

This native packet does not map all nine selected originals. Positive image output/detail shaping, the model-width case, description changes and the exact changing-file read pattern remain outside this packet. The 25 filtered names include EXIF, image preparation, base64 and worker checks. The elapsed-time event-loop case and delayed worker-idle case still need suitable host conditions; no host timing or worker lifecycle result is claimed here.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-tools-images.sh" "$pinned_git_mirror" "$retained_oracle" \
  "$HOME/.npm/_cacache" "$release_archive_dir" > /tmp/pi-v160-tools-images.json
cmp "$inventory/tools-images.json" /tmp/pi-v160-tools-images.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run "TestSourceV160InlineReadImageSafety|TestPinnedImageDetection|TestRead104|Test.*Image" -count=2 -shuffle=42 -p 1 -timeout 120s'
```

The Oracle probe takes the shared lock itself and reads only retained local caches and archives. Its receipt reproduced byte-for-byte. Bypassing only the Go PNG preflight left the final refusal intact but failed the bounded-read assertion. After restoring it, the focused Go tests passed twice with CGO disabled. Vet, Staticcheck `0.8.2`, ShellCheck, and diff checks passed.
