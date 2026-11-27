# Published Codemode rejects an early store write on replay

The unchanged Durable original at `codemode.test.ts:612–630` passed before the native test edit on Node `v26.13.0` and Vitest `4.1.12`; the other 41 file cases were unselected. The prepared test, configuration and lockfile matched Git at `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`. The test SHA-256 was `55453420e8773f75caa8f2167c1965e50553e8934c399d352e53555fe1e9662d`.

The native translation runs the unchanged script through the published v1.6.1 portable guest, a public Harness and SQLite close and reopen. The live journal records one issued call and no results. Only the replay guest's fixture store view changes; the saved start and journal stay intact. The early return produces the source diagnostic `it ended after result 0, where it went on before`, two guest starts, zero replies and one existing nested task. The conversation store contains no values after the refusal.

The supplemental control completes the expected pending call before making the same `store("x", 1)` write. The public Harness observes a successful `late` result, two starts, one reply and the original nested task. The committed conversation store contains exactly `x: 1` under that conversation's ownership. This tests the small inline store layout. Neither case rewrites a saved start or changes owner checks, redaction or lease behavior.

The verified published package root matched fresh scratch extraction of pi-codemode `1.6.1` (`69b391796f465fb7a3432c8aa5b119a0b9983d3c910aa04e2619f7f294e7b7ac`) and QuickJS-WASI `3.6.2` (`f1f4349f19a2d849e33ea0ae9bec2e7062b8839f4eceb17c9051ddbaa2720982`). The original-backed test and the accepted-write control, plus the old early-return and held-batch tests, passed twice with shuffle seed 42 on Go `1.27.3`, CGO disabled. Both runnable scratch controls failed as expected: removing the diagnostic failed the translated original, and dropping store writes failed the admitted fixture's exact `x: 1` assertion. No production edits were needed. The 1 MiB guest setup-admission difference is outside this packet.

## Reproduce the selections

With the verified v1.6.1 Durable Node Oracle and the two published guest roots staged:

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
export GOAI_CODEMODE_V161_PACKAGE_ROOT GOAI_CODEMODE_V160_PACKAGE_ROOT
(
  cd "$oracle/packages/durable"
  flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn \
    node ../../node_modules/vitest/vitest.mjs run test/codemode.test.ts --pool=forks --maxWorkers=1 \
    -t 'ends the call when a rerun without journal entries ends in its first run, where it went on before'
)
flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn make test PACKAGES=./durable \
  'TEST_FLAGS=-v -run "^Test(SourceV161CodemodePublishedWorkerRejectsEarlyStoreReplay|V161CodemodePublishedWorkerAdmittedStoreReplay|SourceV160CodemodePublishedWorkerReplayReturnsBeforeFirstCall|SourceV161CodemodePublishedWorkerHeldBatchBeforeLaunch)$$" -count=2 -shuffle=42 -p 1 -timeout 85s'
bash docs/upstream-v1.6.1/probe-codemode-store-replay.sh
```
