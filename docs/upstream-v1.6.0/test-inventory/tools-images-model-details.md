# Durable image metadata for text-only models

The [Oracle receipt](tools-images-model-details.json) records one pass, no failures or selected skips, and 33 filtered cases from unchanged pi v1.6.0 `packages/durable/test/tools-images.test.ts:147–156`. Omitting the UI detail fails only the selected original at line 153. The probe verifies the pinned source and Photon and pi-ai archives and uses retained dependencies without Node network permission. This original was already in the [inline read receipt](tools-images.json), so the suite's original coverage remains **32 of 34**.

## Native comparison

[`TestSourceV160ModelImageDetails`](../../../durable/tools/image_model_details_upstream_test.go) reads the exact pinned 4×4 PNG through Durable with a text-only model, then with a vision model. The fixture resolves real invocation model capabilities through the public Durable path.

| Source assertion | Native check |
| --- | --- |
| Line 151 | The tool returns one PNG block with exactly the original base64. The settled Durable receipt also retains those bytes. |
| Line 152 | The sole diagnostic says `Read image file [image/png].`, with no note about the model's capabilities. |
| Line 153 | The text-only case carries exactly `{imageUnsupportedByModel: "test"}` as UI details, including in the settled receipt. |
| Line 155 | The vision case has no details object. |

A separate comparison passes the real replay context to the OpenAI Completions serializer. It captures the request body at `OnPayload` and stops before HTTP. The tool's image becomes `(tool image omitted: model does not support images)` beside the normal read diagnostic. The body contains neither PNG bytes nor the UI detail. Transforming the same stored context for a vision model keeps the original image and diagnostic.

The compiled baseline kept the image but attached geometry as UI details and appended a model-capability warning to the diagnostic. The fix replaces those details with the optional model ID and removes the warning. Omitting only the new detail failed the native text-only subtest; vision still passed.

The change leaves the image caps, read paths and structured projection intact. Image geometry still drives the preparation diagnostics; low-level image-loading helpers keep their preparation metadata. Both published Codemode image cases passed with the verified guest and no skips. Focused Go tests passed twice; vet, Staticcheck `0.8.2`, and ShellCheck passed. Both this receipt and the preceding model-width receipt reproduced byte-for-byte. No provider networking, encoder fidelity or Photon worker timing is claimed.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
scratch=$(mktemp)
trap 'rm -f -- "$scratch"' EXIT
bash "$inventory/probe-tools-images.sh" \
  "$pinned_git_mirror" "$retained_oracle" "$user_npm_cacache" "$release_archive_dir" \
  model-details > "$scratch"
cmp "$inventory/tools-images-model-details.json" "$scratch"
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run "TestSourceV160ModelImageDetails|TestSourceV160ModelImageLimits|TestRead120InvocationModelLimitsAndNonvision" -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle runner takes the shared lock itself.
