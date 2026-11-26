# Published Codemode failure receipts and fingerprint refusal

Two unchanged Durable originals passed on Node `v26.13.0` and Vitest `4.1.12` before the native test edit. The original cases were the script-failure receipt (`codemode.test.ts:1094–1100`) and the fingerprint-or-version refusal (`:822–841`). The latter is one reported case that runs both variants. The other 40 file cases were unselected. The SHA-256 of the test compared to pinned Git at `ef4a1cfcca62b77ad3558337ff3e1c7f6beeadfe` was `55453420e8773f75caa8f2167c1965e50553e8934c399d352e53555fe1e9662d`; the Vitest configuration and lockfile also matched pinned Git byte for byte.

`TestSourceV161CodemodePublishedWorkerFailureReportsCalls` runs the actual `1.6.1` portable guest in the existing Node subprocess bridge, with real nested tools and SQLite. After `echo` succeeds and the script throws, the receipt is an error, starts with `Script failed`, contains `Error: boom`, and prints exactly one `Tool calls made before the failure (they are not undone): echo (ok)` line. The call, guest delivery and guest start each happen once. Native v1.6.0 grouped and legacy-journal tests keep the same checks.

`TestSourceV161CodemodePublishedWorkerFingerprintRefusal` tests the fingerprint variant. The published guest starts and reaches the pending `hold`. After SQLite close and reopen, the test changes the host sandbox identity, leaving the saved start intact. The refusal is exactly `durable: rejected: the script cannot resume: the sandbox changed since it started` in the harness error block. It preserves the saved start and journal values, starts no second guest, delivers no reply, and never invokes the late `echo`. One existing nested task remains; the check does not constrain when that task recovers. The source's version-0 variant stays partial because native Storage refuses the start rewrite. The existing v1.6.0 immutability test separately verifies that refusal and then resumes the unchanged start; it is not evidence that native runs the version-0 source scenario.

The published package root matched fresh scratch extraction of both retained archives. The pi-codemode `1.6.1` archive SHA-256 was `69b391796f465fb7a3432c8aa5b119a0b9983d3c910aa04e2619f7f294e7b7ac`; the QuickJS-WASI `3.6.2` archive was `f1f4349f19a2d849e33ea0ae9bec2e7062b8839f4eceb17c9051ddbaa2720982`. This uses the portable entry, not the standalone Node worker topology.

All six focused native tests passed three times with shuffle seed 42 on Go `1.27.3`, CGO disabled. Vet and Staticcheck `0.8.2` passed for `./durable`. Two scratch Go overlays failed as expected: changing the call receipt text failed its exact line comparison, and bypassing the first identity check failed the exact refusal comparison with `invalid remote codemode runtime` from the downstream guard. No production code, saved start, retained package or upstream test bytes changed.

## Reproduce the selections

With the previously verified v1.6.1 Durable Node Oracle and the two published guest roots staged, run:

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
export GOAI_CODEMODE_V161_PACKAGE_ROOT GOAI_CODEMODE_V160_PACKAGE_ROOT
(
  cd "$oracle/packages/durable"
  flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn \
    node ../../node_modules/vitest/vitest.mjs run test/codemode.test.ts --pool=forks --maxWorkers=1 \
    -t 'reports a failed script with its error and the calls already made|refuses to resume when the sandbox.s fingerprint or the code mode version changed'
)
flock /tmp/go-ai/v160-focused-verify.lock unshare -Urn make test PACKAGES=./durable \
  'TEST_FLAGS=-v -run "^Test(SourceV16[01]CodemodePublishedWorker(FailureReportsCalls|FingerprintRefusal)|V160CodemodeLegacyWorkerFailureReportsCalls|V160CodemodePublishedWorkerStartVersionImmutable)$$" -count=3 -shuffle=42 -p 1 -timeout 80s'
```
