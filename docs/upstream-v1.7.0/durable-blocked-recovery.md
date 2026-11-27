# Unchanged Durable blocked scheduler and recovery

This bounded packet ports the deterministic SQLite and public scheduler behavior in unchanged pi v1.7.0 `packages/durable/test/harness-blocked.test.ts` at `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The isolated lane starts at exact native main `59c6ed7e4cd8c0ff73236615d345d8d974060d4d`. The runtime fix is `d52fdefded37a08628480f25190037b88aea8148`.

## Pinned original

The test matched the pinned Git blob and SHA-256 `a1db2d074009d7249afdd64ec26fa29c60b1182b57c21525dc4d76fcabd58b8d`; the staged checkout remained at the pin. On Node `v26.13.0` and Vitest `4.1.12`, the unchanged original passed all **12/12** tests with no skips. It ran under `/tmp/go-ai/v170-shared-verification.lock` with a 650 MiB Node heap cap and `GOMEMLIMIT=650MiB`.

```sh
cd "$PI_MIRROR/packages/durable"
NODE_OPTIONS=--max-old-space-size=650 GOMEMLIMIT=650MiB \
  ../../node_modules/.bin/vitest --run test/harness-blocked.test.ts \
  --maxWorkers=1
```

## Exact comparison and proven gap

The native SQLite/public-path controls cover recovery wake ownership, restart-limit disposition and reporting, clean-close requeueing, abort admission before close, interrupted abort handlers, live public settings changes, identical-checkpoint progress accounting, progress reset, migration blocking, abandoned-task definition waits and registry-change wakeup.

The translated recovery regression first failed on the unchanged native tree: a real SQLite append rejection during `Open` returned the original storage error but delivered no `OnWake` event, while the pinned original requires the host to retain one active wake. The fix publishes only `{active:true}` after owned failure cleanup when durable running-task recovery is still pending. It does not manufacture a balancing inactive wake, retain a `Harness`, bypass storage error identity or write task data.

No other production difference was demonstrated. Restart counts, task state, abort marks and SQLite records remain governed by the existing scheduler/storage transaction paths. The change does not alter the schema, migration format, task ownership, cancellation admission, checkpoint contents or reporting metadata. The wake value contains no task, input or checkpoint fields.

## Native evidence

The following selector passed twice with different shuffle seeds after the fix:

- `TestHarnessBlockedRecoveryWakeSurvivesRejectedCommit`
- `TestHarnessBlockedRegistryChangeUnblocksWake`
- `TestRestartLimit150RecoveryDispositionAndHostControls`
- `TestClose140RequeueDoesNotConsumeRestartBudget`
- `TestTaskRecoveryDirectAbortCloseReopenFourStages`
- `TestRestartLimit140InterruptedAbortFaultsBeforeDefinitionResolution`
- `TestRestartLimit140ProgressNotIntentOrMemo`
- `TestTaskGraph150BlockedMetadataWithoutEffectsOrCommits`
- `TestTask120AbandonOnRestartMissingDefinitionWaitsForCleanup`
- `TestRestartLimit150MissingDefinitionCutoffWakesAndFaults`
- `TestSourceV160HarnessLateRegistryDefinition`

Separate ownership/cancellation/redaction controls also passed:

- `TestTaskRecoveryHostCallbackFencesPreserveNormalErrorIdentity`
- `TestTask120ExplicitAbortSupersedesRestartBlockedChild`
- `TestTaskOwnershipCancelledOwnerLateChildConversationWorkAfterReopen`

All native runs used pure Go with `CGO_ENABLED=0`, `GOFLAGS=-p=1`, `GOMAXPROCS=2`, `GOGC=60`, `GOMEMLIMIT=650MiB`, the shared verification lock and the Go 1.27.3 toolchain. The focused selectors completed in 5.046 and 3.256 seconds; controls completed in 0.530 seconds.

`go vet ./durable` passed. Pinned Staticcheck `v0.8.2` passed for `./durable`. The original 12 tests and the new native SQLite controls provide deterministic source evidence; they do not establish full acceptance or acceptance profiling.

## Limits

JavaScript `Harness.closed` is represented by native `Open` returning its original error and no `Harness`; typed Go inspection/report fields replace object equality where the public values are equivalent. The assertion ledger identifies those shape adaptations. The packet does not claim broader provider, retention, migration, release or full-suite acceptance.
