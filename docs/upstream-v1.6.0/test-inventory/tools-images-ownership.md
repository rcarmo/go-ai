# Durable prepared image equality and input ownership

The [runtime receipt](tools-images-ownership.json) reruns the unchanged pi v1.6.0 case at `packages/durable/test/tools-images.test.ts:431–442`. It records one pass, no failure or selected skip, and 33 filtered cases. That original was already counted in the [preparation receipt](tools-images-preparation.json); this does not change the 32-of-34 selection total.

The case compares the entire prepared values returned by the real worker and the calling-thread processor, and compares the caller's PNG against its original copy. The Photon witness observed the image worker load 139,647 bytes of JavaScript and instantiate 1,880,773 bytes of WASM, both matching the lock-verified archive. It also observed the worker consume the 4,841,533-byte 1100×1100 PNG and reply with a 1,644,243-byte JPEG, `convertedFrom: "image/png"`, and no resize metadata. The worker reply rules out fallback to calling-thread processing. The calling thread's fixture separately initialized the verified WASM. The probe uses an empty credential environment and Node file permissions scoped to scratch, with no Node network permission.

Adding only a JavaScript comment and a WASM custom section left the prepared output and the original assertions unchanged but failed both asset comparisons. Transferring the caller's original buffer instead of a copy made the unchanged case fail at line 440: the calling-thread processor no longer had the original bytes.

## Native comparison

[`TestSourceV160PreparedImageOwnership`](../../../durable/tools/image_ownership_upstream_test.go) uses the public Go `NativeImageProcessor`, `PrepareImage`, and `FitImage` APIs. It compares every native prepared field, including the exact encoded bytes, and checks that the fitted content contains those bytes as standard base64 with the corresponding format diagnostics. It verifies input ownership after each call, output dimensions, the byte cap, and the absence of resize metadata.

The always-on native case encodes the original pixel pattern with Go. The second case, enabled by the reproduction command below, verifies the SHA-256 of the **exact PNG received by the Photon worker** and checks that its decoded pixels match the native fixture.

The exact result comparison is **between entry points within each runtime**, as at line 440. Cross-runtime codec choice differs on this input: Photon emits JPEG, while Go's lossless re-encoding fits under the same cap and returns a 66,171-byte PNG. Both keep the dimensions. The separate PNG-to-JPEG assertion at line 218 is not closed, and this packet does not change the encoder to force that result. Go does not run a Photon worker; the event-loop gap and idle-worker restart originals at lines 404 and 444 remain open.

Both native cases passed on the unchanged baseline. Two temporary faults failed both cases: changing one caller byte triggered the ownership check, and corrupting one returned byte triggered the full-value comparison. Restored focused tests passed twice with CGO disabled, along with vet, Staticcheck `0.8.2`, and ShellCheck. A second stable run reproduced the captured PNG and runtime events. The runner's existing resize receipt also reproduced byte-for-byte.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp -d)
trap 'rm -rf -- "$scratch"' EXIT
bash "$inventory/../dependencies/probe-photon-runtime.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  stable-worker "$scratch/input.png" > "$scratch/receipt.json"
cmp "$inventory/tools-images-ownership.json" "$scratch/receipt.json"
source scripts/project-env.sh
GO_AI_PINNED_PHOTON_INPUT="$scratch/input.png" \
  flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run TestSourceV160PreparedImageOwnership -count=2 -shuffle=42 -p 1 -v -timeout 90s'
```

The Photon runner acquires the shared lock itself. Its optional last argument exports the exact worker input to a new file; it does not overwrite an existing file. The file is temporary and is not checked in.
