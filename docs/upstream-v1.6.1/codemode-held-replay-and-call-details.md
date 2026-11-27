# Published Codemode held replay and call details

Two unchanged Durable originals passed before the native test edit on Node `v26.13.0` and Vitest `4.1.12`: call details (`codemode.test.ts:448–472`) and all-or-none held replay (`:632–661`). The other 40 file cases were unselected. The prepared test, configuration and lockfile matched Git at `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe`; the original test SHA-256 was `55453420e8773f75caa8f2167c1965e50553e8934c399d352e53555fe1e9662d`.

Both native translations ran unchanged production through the published v1.6.1 portable subprocess, a public Harness and SQLite:

- **Call details:** the model receipt contains three ordered rows: `echo` succeeded, `fails` returned `permission denied`, and `hold` was cancelled. The test compares IDs, names, exact short JSON previews and statuses. The first call has a nonnegative duration. There is one guest start and there are two deliveries.
- **Held replay:** the first guest records a two-call batch with no delivered results. The test changes only the second guest's fixture store view, so both call positions conflict. The grouped replay checks the batch before admitting work: no new echo, one original nested task, two guest starts, zero deliveries, and unchanged saved start and journal. The diagnostic identifies the second call and both tool targets, `call 2 is tools.hold(…), but was tools.hold(…)`. This assertion remains partial: the source prints literal argument values, while Go preserves the existing redaction. The legacy journal and single-mismatch tests are supplemental controls, not additional original variants.

The published package root matched fresh scratch extraction of both retained archives. pi-codemode `1.6.1` SHA-256 was `69b391796f465fb7a3432c8aa5b119a0b9983d3c910aa04e2619f7f294e7b7ac`; QuickJS-WASI `3.6.2` SHA-256 was `f1f4349f19a2d849e33ea0ae9bec2e7062b8839f4eceb17c9051ddbaa2720982`. This uses the portable entry, not the standalone Node worker topology.

The two new tests, the v1.6.0 equivalents, and the existing schema-tool failure receipt test passed three times with shuffle seed 42 on Go `1.27.3`, CGO disabled. Vet and Staticcheck `0.8.2` passed for `./durable`. Three isolated scratch Go overlays failed as expected: marking an undelivered call successful, executing the fixture's new echo before checking the later mismatch (one echo and two nested tasks), and suppressing the later conflict's diagnostic (the host reported call 1 instead of call 2). The controls were local to disposable fixtures with no egress. Production source, saved starts, ownership checks, redaction and retained assets were unchanged.

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
    -t 'lists the script.s tool calls in its result.s details|checks every call a rerun holds before launching any of them'
)
flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn make test PACKAGES=./durable \
  'TEST_FLAGS=-v -run "^Test(SourceV16[01]CodemodePublishedWorker(CallDetails|HeldBatchBeforeLaunch)|CodemodeCallDetailsRecordsStructuredFailureV160)$$" -count=3 -shuffle=42 -p 1 -timeout 105s'
bash docs/upstream-v1.6.1/probe-codemode-held-replay-and-call-details.sh
```
