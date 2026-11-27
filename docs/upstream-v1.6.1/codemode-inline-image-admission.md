# Measured Codemode inline-image admission gap

All three unchanged `accepts wrapped and large base64 image data` originals passed on Node `v26.13.0` and Vitest `4.1.12` before the native test edit. The worker, inline and remote originals ran; the other 153 cases were unselected. The test, Vitest configuration and lockfile matched Git at pi v1.6.1 `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. The test file SHA-256 is `a76f2e0f3688286fcb15de79a675cc05bae699117e5f740b12dd26dbb0090931`.

The original cooked JavaScript template is **1,048,684 bytes** (SHA-256 `fbf988167fb2e02bcad9e00b83fb418ee13c71ece35086fcdc1ff2f7813bc7de`), including its whitespace and escaped wrapped image. The test passes these exact bytes to the archive-verified published v1.6.1 package through its inline, portable remote and standalone Node worker entries. Each succeeds and returns two exact PNG blocks in order. Their base64 payloads are **12** and **1,048,588** bytes, with SHA-256 `929e08d597feae564ce98003c4a47ff5239a5b93681d186eda3a9871e5b62644` and `320a0f98e2e0bafb2660a8b48d049ec2b0ceef37d27d8e4f59794c25ac597609` respectively. These are image-output checks, not image decoding.

The same original through public Go `QuickJSSandbox.Start` and its result stream is **108 bytes above the native setup cap**. The first result is `sandbox: invalid or oversized QuickJS setup input`, with no return value, no images, and zero reservations or module starts. The refusal includes the exact script-error text and no-call summary.

| Native source | Bytes | Result |
| --- | ---: | --- |
| Exact pinned literal | 1,048,684 | Refused before worker capacity |
| Generated payload script, padded one byte over the cap | 1,048,577 | Same refusal, no module starts |
| Same script, padded one byte under | 1,048,575 | Two exact PNG blocks, one module start |
| Same script, padded to the cap | 1,048,576 | Two exact PNG blocks, one module start |

The three padding controls keep the executable script and expected image bytes the same; only source length changes. The below-cap controls are supplemental and do not close either source assertion for the native original. The ledger leaves both partial. No limits, setup code, or published assets changed.

The published root matched extraction of the retained pi-codemode `1.6.1` archive (`69b391796f465fb7a3432c8aa5b119a0b9983d3c910aa04e2619f7f294e7b7ac`) and QuickJS-WASI `3.6.2` archive (`f1f4349f19a2d849e33ea0ae9bec2e7062b8839f4eceb17c9051ddbaa2720982`). Both new Go tests and the v1.6.0 equivalents passed twice on Go `1.27.3` with CGO disabled. Vet and Staticcheck `0.8.2` passed for `./durable`.

## Reproduce the selections

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
export GOAI_CODEMODE_V161_PACKAGE_ROOT GOAI_CODEMODE_V160_PACKAGE_ROOT
(
  cd "$oracle/packages/codemode"
  flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn \
    node ../../node_modules/vitest/vitest.mjs run test/sandbox.test.ts --pool=forks --maxWorkers=1 \
    -t 'accepts wrapped and large base64 image data'
)
flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn make test PACKAGES=./durable \
  'TEST_FLAGS=-v -run "^Test(SourceV16[01]CodemodePublishedLargeImageData|V161CodemodeNativeInlineImageAdmission|V160CodemodeNativeLargeImageData)$$" -count=2 -shuffle=42 -p 1 -timeout 80s'
```
