## Durable test portability

The durable tests now compile and run with CGO disabled while retaining generic receipt and task-ownership coverage. Only SQLite-specific helper implementations use build constraints; no generic test file is excluded and no skip is added.

Previously, three untagged files referenced `OpenSQLite`, `SQLiteOptions` or `*SQLiteStorage`: `receipt_utf16_main_test.go`, `task_owned_inspect_reopen_test.go` and `utf16_backends_main_test.go`. SQLite's implementation is CGO-only, so even a non-SQLite test filter failed during compilation.

`utf16Backends` still runs memory and journal cases. Its build-specific extension registers SQLite only when available. The CGO extension supplies checked close/reopen handling; cleanup accepts ErrOwned only after the owning harness has joined storage closure. The receipt persistence test now uses this shared backend helper, retaining its exact-integer, UTF-16 witness, deduplication, mutation-isolation and authority/budget assertions. Owned-conversation inspection retains both marked and foreground-hold variants.

### Verification

Go 1.27.3/GOMAXPROCS=4, owned empty HOME/APPDATA and project-namespaced temporary state:

- Full root normal tests and root vet pass with CGO_ENABLED=0 and CGO_ENABLED=1.
- Focused receipt/ownership tests pass under CGO-enabled race checking.
- Parsed focused test events contain the same 15 generic named tests/subtests in both configurations, with no skips. CGO adds exactly these three cases:
  - `TestReceiptUTF16MainContributionAndStores/sqlite`
  - `TestTaskOwnershipPinnedReopenedOwnedConversationInspectionAndIdle/marked/sqlite`
  - `TestTaskOwnershipPinnedReopenedOwnedConversationInspectionAndIdle/foreground-hold/sqlite`
- Module verification and dependency/accepted-asset tripwires pass. No production code, dependency or release changes.

The full durable race suite reached a 420-second suite limit while `TestBackgroundSubagentIdleBoundaryAndReopenDedup/journal` had just begun (parent active for one second). That exact subtest passes three independent race repetitions in 1.888 seconds total. This follows the earlier 180-second suite timeout; the full-suite race gate is still open. A later verification run needs a measured test schedule or suitable overall budget; isolated success is not full-suite success. No pre-release profiling was performed for this test-only refactor.

Disposable test event logs were removed immediately after comparison. Failed-run fixtures/build scratch were removed after confirming no active test processes. Source group-commit preparation, receipts, deferred persistence and continuation ordering remain [open](durable-commit-barriers.md).
