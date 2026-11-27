# Codemode portable batching on pi v1.7.0

The retained pi-codemode `1.7.0` and `1.6.1` archives have **64 byte-identical `dist` files**. Their extracted trees differ only in the package manifest's version. The v1.7.0 archive matched the registry SHA-512 and the [pinned input SHA-256](inputs.json) `423b548f690cab0800d1fccf0bb034108cf535652a5072e0bf35f84ed0a919d1`; the v1.6.1 archive SHA-256 is `69b391796f465fb7a3432c8aa5b119a0b9983d3c910aa04e2619f7f294e7b7ac`. The locked QuickJS-WASI `3.6.2` entry is identical in both locks. Its retained archive passed that SHA-512 check, and its 637,405-byte WASM member matches the Go embedded asset (SHA-256 `d4c9375f2b1ca4dc95f72c8aa2982a7a9951ac8011490d79c6582df732b4bbd9`). These comparisons identify input bytes, separately from the execution below.

All **seven unchanged** `packages/codemode/test/portable.test.ts` cases passed before the native test edit on Node `v26.13.0` and Vitest `4.1.12`, with no failures or skips. The private stage used Git source and config from `fb0fe823539f6d04850f6e34748c01f01f212e1f`; the test's SHA-256 was `e80323f38106e4f63590a7ff727965c3b450422b95f6de679d9960b7d9db784f`. The private stage reused 44 host-compatible runner placements from v1.6.1 after verifying that all their lock entries were identical to v1.7.0 before execution. The private Oracle had no network access and no package install ran for this receipt.

This packet translates the **portable output batching** case only (`83–91`). The old native code passed with the real v1.7.0 `serveCodemodeRemote` subprocess and the existing public Go `RemoteCodemodeSandbox` adapter. The test executes the exact original loop, checks result `ok`, all 5,000 ordered console messages including the final `4999`, and no more than six worker exchanges. The observed count was six. It also checks the final zero-call drain and the exact native console projection. The guest fingerprint is pinned before launch and matches the unchanged compiled guest artifacts. This does not cover the standalone Node worker path, cloud hosts, or native mappings of the other six originals.

The runnable scratch controls drop the last console item from the Go projection or halve the copied guest's flush size. The former expects a projection mismatch despite all 5,000 worker items; the latter expects 11 exchanges for the same items. No production, retained asset, setup, saved-start, or disclosure policy changes are part of the packet.

## Reproduce the selections

Use a private `git archive` stage of the pinned root `package.json`, `package-lock.json`, Codemode `src`, package manifest, `vitest.config.ts` and `test/portable.test.ts`. Copy the retained Vitest runner closure only after comparing its placements' lock rows between the two pins, and extract the verified QuickJS-WASI archive into the private `node_modules`. For Go, extract the package and QuickJS archives into `node_modules/@earendil-works/pi-codemode` and `node_modules/quickjs-wasi` under the two separate roots, then run:

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
export GOAI_CODEMODE_V170_PACKAGE_ROOT GOAI_CODEMODE_V161_PACKAGE_ROOT
(
  cd "$oracle/packages/codemode"
  flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn \
    node ../../node_modules/vitest/vitest.mjs run test/portable.test.ts --pool=forks --maxWorkers=1
)
flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn make test PACKAGES=./durable \
  'TEST_FLAGS=-v -run "^Test(SourceV170CodemodePublishedPortableOutputBatching|SourceV161CodemodePublishedWorkerCallDetails)$$" -count=2 -shuffle=42 -p 1 -timeout 80s'
bash docs/upstream-v1.7.0/probe-codemode-portable-batching.sh
```
