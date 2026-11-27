# Codemode store checkpoints preserve as-of forks

Both cases in the byte-verified, unchanged `packages/durable/test/codemode-checkpoints.test.ts` passed on Node `v26.13.0` and Vitest `4.1.12` before the native test edit. There were no skips or failures. The test, Vitest configuration and lockfile matched Git at pi v1.6.1 `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`; the test's SHA-256 was `9409c19a882393d3bf656648691b74157de8bc179dd21137d58fa3a9e0415445`. This packet maps the store case only (`14–60`); the task-scoped declaration case ran upstream but is not mapped here.

The native translation calls the public Harness, transaction and Storage read methods against SQLite. Counter `0` is the initial base. Writes `1–64` produce 64 deltas. Write `65` resets the delta count to zero and the public receipt contains exactly one `document.change`, a version-1 base of `{values:{counter:65}}` for the same document. The counter-32 entry still reads `{values:{counter:32}}` and a fork at that entry starts with the same value. Write `66` starts a new delta chain of length one. The translation passed without production changes.

As a supplemental check, close and reopen SQLite, then read `load("counter")` through the published v1.6.1 portable guest in the parent and fork. The public model receipts contain exactly `66` and `32`, respectively, with two guest starts, zero nested replies and four model requests. The guest reads the committed values; it does not generate the checkpoint writes. The package root matched fresh scratch extraction of the retained pi-codemode `1.6.1` archive (`69b391796f465fb7a3432c8aa5b119a0b9983d3c910aa04e2619f7f294e7b7ac`) and QuickJS-WASI `3.6.2` archive (`f1f4349f19a2d849e33ea0ae9bec2e7062b8839f4eceb17c9051ddbaa2720982`).

The translated case, historical checkpoint test and both release fork checks passed twice with shuffle seed 42 on Go `1.27.3`, CGO disabled. Both runnable scratch overlays failed as expected: selecting the base one delta early returned zero where 64 deltas were expected, and changing the store fork policy to current returned `65` instead of `32`. No production code, setup cap, saved starts, redaction, callback accounting, or ownership semantics changed.

## Reproduce the selections

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
export GOAI_CODEMODE_V161_PACKAGE_ROOT GOAI_CODEMODE_V160_PACKAGE_ROOT
(
  cd "$oracle/packages/durable"
  flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn \
    node ../../node_modules/vitest/vitest.mjs run test/codemode-checkpoints.test.ts --pool=forks --maxWorkers=1
)
flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn make test PACKAGES=./durable \
  'TEST_FLAGS=-v -run "^Test(SourceV161CodemodeStoreCheckpointAndFork|Codemode140StoreCheckpointThresholdAndHistory|SourceV16[01]CodemodePublishedWorkerForkScopedStore)$$" -count=2 -shuffle=42 -p 1 -timeout 80s'
bash docs/upstream-v1.6.1/probe-codemode-store-checkpoint.sh
```
