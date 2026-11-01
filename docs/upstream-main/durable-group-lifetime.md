## Group queue and process-crash controls

The internal group primitive now has three queued read/writer cases, six SQLite process-crash cases and five journal process-crash cases. Session still submits one batch at a time. No public group API, storage format or dependency changes.

### Queued operations

`storage_group_concurrency_test.go` holds the first receipt while the core still owns the whole call. A raw Snapshot reader and another caller's write both attempt admission before release. Neither settles early; the first call's three receipts precede the other caller's receipt. A reader can observe seq 3 or 4 depending on which queued operation enters next, but must contain all three earlier batches. No Go FIFO scheduling assumption is needed.

Memory/journal have adopted only their first batch at the hold; SQLite has committed/adopted the whole prefix. The same gate covers both states. These are separate callers on one core, not separate source StoreStorage owners sharing a parent Store.

### Process recovery

`storage_group_crash_sqlite_test.go` launches the test executable with a restricted environment and an owned working directory. It exits without Close at three boundaries, each with the empty and `worker_` prefixes:

1. SQLite COMMIT returned, before core adoption or any receipt.
2. The first receipt arrived; later receipts and the call result did not.
3. The call returned and another unused ID was reserved.

The parent waits for process termination, then opens through normal native path/ownership locks. All three batches, their seq/position/value fields and allocator high-water survive. The parent mints a non-reused ID and appends the next seq. A small receipt-count marker distinguishes the boundaries; the pre-receipt exit must leave no marker. No ownership fence is bypassed or live dataset opened.

`storage_group_crash_journal_test.go` uses the same joined-child/restricted-environment/owned-path pattern with the journal's default synchronous policy. Five boundaries recover distinct prefixes:

| Exit boundary | Stored batches | Delivered receipts | Allocator high-water |
| --- | ---: | ---: | ---: |
| First frame append returns, before core adoption | 1 | 0 | 4 |
| Second frame append returns, before its core adoption | 2 | 1 | 4 |
| First receipt | 1 | 1 | 4 |
| Call returned, then unused ID reserved | 3 | 3 | 5 |
| Third batch refused normally | 2 | 2 | 4 |

The parent checks every stored entry's seq/position/value, the absent suffix, receipt-count evidence, non-reused IDs and the next appended seq. Journal's confirmed prefix differs from SQLite's atomic whole prefix; a receipt never implies that later journal batches were stored. Exiting before the second frame append in an overlay mutation fails the second-before-adoption recovery test. The overlay is deleted after analysis.

These are abrupt process-exit checks. They do not simulate machine power loss, torn device writes or a crash inside COMMIT. Source cross-owner/export isolation still needs work. Native has no corresponding streaming Store export surface. Batch-container collection needs a runtime-appropriate check; Go's weak-pointer documentation does not promise eventual nil in general, so a collection probe must address object layout/liveness and include a retention mutation control instead of weakening that gate.

### Checks

Go 1.27.3/GOMAXPROCS=4, project-owned temp roots and empty homes:

- The two non-CGO queue cases pass five initial normal repetitions with shuffle 42. The three CGO queue cases and six crash cases pass normal checks.
- Queue/crash plus context/timer/ownership checks pass three race repetitions each with shuffle seeds 7 and 42. Helper subprocesses use the same race-instrumented binary; race checking is not disabled.
- The [context-retention fix](durable-harness-scopes.md) adds twelve CGO or eight non-CGO cases. Together this increment has 21/10 substantive leaves; the child-entry helper is excluded from those counts.
- Full root normal tests and root vet pass in both CGO modes; module verification passes. Scoped staticcheck 0.8.2 reports only the existing usage_json.go:11 U1000 finding. The broader staticcheck gate remains open. The later full durable race gate passes at a5dedbd in 631.158 seconds; see [publication-image checks](durable-publication-image.md).

The five journal cases also pass three normal repetitions with shuffle 7, fresh root normal/vet checks in both CGO modes, and three race repetitions alongside SQLite crash/group tests with seeds 7/42. Module verification passes. These are development correctness checks; the full durable race run above predates the five new journal cases.

Disposable test databases/journals, child receipt markers, logs and copied source excerpts are removed at test cleanup or immediately after analysis. Accepted assets and v1.4.0 are unchanged. Pre-release profiling and full acceptance are unfinished.
