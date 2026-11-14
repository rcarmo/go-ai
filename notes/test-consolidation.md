# Durable test consolidation

## Task/Storage source-port consolidation

The current merge combines22 related files into6 version-neutral suites:

| Suite | Input files | Scope |
|---|---:|---|
| `durable/storage_public_test.go` | 7 | Public command wire/replay, root/ID boundaries and refusal values |
| `durable/storage_tasks_test.go` | 3 | Task record wire, raw builtin records and original Storage task conformance |
| `durable/transaction_tables_test.go` | 3 | Source table read/write fences, task table ownership and publication |
| `durable/builtin_tasks_test.go` | 3 | Canonical builtin publication, state progression and UTF-16 ownership |
| `durable/scan_test.go` | 3 | Ordered scans, captured cursor wire and SQLite reopen |
| `durable/storage_requests_test.go` | 3 | Recoverable request validation and direct SQLite request matrix |

Ordinary durable test files fell **613→597**. During the mechanical merge, all **1,467 target declarations** and **2,190 non-import declarations** matched exactly after Go parsing/formatting without comments. Package identity is included; testdata is excluded. No build constraints were merged away. Source pins remain in comments/fixtures; active paths and register links were updated. The source-port edits before this merge and the later strengthened Session getter assertion are separate semantic changes.

- Sorted target-declaration SHA-256: `21efc8fac30f3ac6213bfd5242974c28c8189e405331f6c845dc94b3671ab722`.
- Sorted normalized-declaration SHA-256 at the mechanical boundary: `d8296f1036faa9e1fc957daad77cf71efef1311755463b1bf1638488899004bd`.
- Final full durable418.099s/tools19.320s;10 shuffled focused repetitions25.751s; vet/Staticcheck/logging and four semantic fault controls pass.
- Reference148 unchanged source cases +37 independent probes pass; full upstream assertion/release acceptance remains open.

Temporary parser/merge tools and inventories were deleted after comparison. Continue consolidating related suites while porting; do not create release-specific files for individual deltas.

## Earlier Session/tool consolidation

Related Session and tool-integration tests were merged without changing test names,
assertions, helpers or build constraints. Production code is unchanged by this move.

| Suite | Input files | Result |
|---|---:|---|
| Session admission, batching and dependency reads | 4 | `durable/session_group_commit_admission_test.go` |
| Outcome fences, capacity and failure boundaries | 5 | `durable/session_group_commit_outcomes_test.go` |
| Document cache and idle retention | 2 | `durable/session_document_cache_test.go` |
| Key forms and transaction write footprints | 2 | `durable/session_keys_v150_test.go` |
| Source202 watch folding and threshold assertions | 2 | `durable/session_watch_v150_test.go` |
| Public tool integration and failed bash receipts | 2 | `durable/tools/integration_test.go` |

The total under `durable/` fell from **621 to 610 test files**. Active crosswalks and
file references point to the consolidated suites. Historical patch captures retain
their original filenames and are already labelled superseded.

## Preservation checks

Before and after the move:

- All **1,402** discovered test, benchmark, example and fuzz-target names matched.
- All **2,093** non-import declarations matched after parsing and formatting without
  comments. This compares function bodies, helpers, types, constants and variables,
  not just the number of tests. Imports and file placement were the only code changes.
- The Unix build constraint on tool integration was retained.

SHA-256 of the sorted discovery inventory:
`7bad1f92891dfa235866eead2c2b25c88f7d43ae6cd9b119dc485c3cd334eb58`

SHA-256 of the sorted normalized-declaration inventory:
`1a7d2a41ba3ccaa2a90cf2088819ecf58f03221827b2f800460960742c0da983`

Disposable inventories and diagnostic files were removed after comparison. The
hashes and these conclusions are retained instead of raw run archives.

## Verification

- `make test PACKAGES='./durable/...' TEST_FLAGS='-count=1 -p 1 -timeout 900s'`:
  durable **378.557s**, tools **18.368s**, both passed.
- Repository-wide `make vet` and `make staticcheck`: passed.
- `make check-logging`: passed.
- Pinned-source oracle runner: **72 unchanged upstream tests + 5 probes**, passed.

This is the first consolidation batch. It does not close the upstream parity
register or imply that all remaining test files should become one large suite.

## Separate runtime and mechanical commits

The task-table runtime port is `bb1d20f71bd2ca29c4209b51269938ab3c80e695`; the mechanical test merge and path updates are `8b3936e6dc2edf2c93900c545740c756c7ed466f`. Before committing, the pre-merge staged tree and final tree were independently compared: all2,190 normalized declarations match, including the later stronger Session getter assertion. Final declaration SHA-256: `442f3a95feab2a89a071c22f4ee7d5ea8468410231bb19854740247c78c1741f`.

The staged pre-merge tree also compiled and passed the focused source-task/table, invariant, cursor and published-image selection in2.884s. Its temporary archive and parser files were deleted. Final-tree full-suite/static/fault receipts are recorded above.

## Primary-read history consolidation

After runtime commit `394109a`, hook-reader and after-run history suites merge
into `durable/task_history_test.go` (two files→one). Source pins, all three test
functions and the literal kind helper remain unchanged. The full durable test
inventory, including unchanged build-constrained files, is checked on both sides
of this mechanical boundary; neither input has build constraints.

- Files: **597→596**; targets: **1,473**; non-import declarations: **2,198**.
- Sorted target SHA-256: `65ac16eb7425f0c25c9afb31c1a4df24c2984c6c98275b76f10c1e5f1057c5a7`.
- Sorted normalised-declaration SHA-256: `affdab7f0bc1c3506a06d5bd1ccff0210aaa40a20405a70b5da796218dfe3e48`.
- Both trees match exactly. The pre-merge inputs are read from committed
  `394109a`; all other tests are unchanged. Eight build-constrained files are
  included in the inventory and unchanged; pure-Go verification does not imply
  their excluded configurations ran.
- Five shuffled focused repetitions after the move: **4.545s**. Full runtime
  verification before the move is recorded in [primary reads](primary-record-reads.md).
  Register/source-hash checks and diff hygiene pass. No production code changes.

Temporary parser, inventories and merge scripts are deleted after comparison.

## Submission-reader consolidation

After runtime `22c7716`, four related suites merge into two version-neutral suites:

- `submission_records_test.go` absorbs `submission_presence_v150_test.go`.
- `transaction_tables_test.go` absorbs `submission_transaction_test.go`.

Source pins, tests, assertions and helpers are unchanged by this mechanical
step. Neither input merge has build constraints. The inventory includes the
unchanged build-constrained files; their excluded configurations are not implied
to have run.

- Files: **597→595**; targets: **1,484**; non-import declarations: **2,214**.
- Sorted target SHA-256: `79f6d64ead7983839036efbbdfe945243f166b204c7a502c4768d6c64925b1e6`.
- Sorted normalised-declaration SHA-256: `310dd6731010b52d735bb41fcf90c26c5d470c0fbe8f59dd16fd6135995b2e8f`.
- Pre-/post-merge inventories match exactly. Five post-merge shuffled repetitions
  pass in **4.960s**; register/source-hash checks and diff hygiene pass.
- `task_reads.go` is renamed to `record_reads.go`, reflecting its actual task,
  conversation, entry and submission readers. Contents are byte-identical to the
  runtime commit; active register paths are updated.

The parser, inventories and merge scratch are deleted after comparison.
[Submission-read receipts](submission-record-reads.md) retain the full pre-move
runtime verification and its remaining scope.

## Compaction lifecycle consolidation

After runtime0b13193, compaction_status_test.go absorbs the failure/reopen/no-cut/summary test from compaction_failure_120_test.go. Both test targets and the status helper remain unchanged. The destination equals the previous status suite followed by the byte-identical moved declaration after gofmt. Its SHA256 is 672e2e476512fcf2df2f073f6b5c8c91dea48dd5d615f5a915269853445801a1. Neither file has build constraints; production code is unchanged.

The recursive durable *_test.go inventory (including nested fixture and constrained files) falls599→598. Ten shuffled repetitions of both tests pass4.320s; vet, Staticcheck, logging, register and diff hygiene pass. Runtime full-suite evidence remains430.208s/tools20.080s before this mechanical move. Active v1.5 assertion/register paths and the older crosswalk's current-file references are updated. No temporary merge/parser artifacts remain.

## Model capability and paid-call consolidation

After runtime96612a58921537190d9e4ada3f9a943e62201cd4, eleven related files become four version-neutral suites:

- models_test.go: model capability/catalogue, typed catalogue, authentication and telemetry.
- models_paid_test.go: paid validation/results, options and image/classifier lifecycle.
- models_paid_fault_test.go: classifier/image paid-effect settlement faults.
- codemode_models_test.go: renamed code-mode model globals/envelopes.

All bodies, source-pin comments and targets are retained. Imports are merged with alias checks; no input has build constraints. The recursive durable inventory includes unchanged constrained and nested fixture files. Native path references in active and older crosswalks now point to the current suites; duplicate path references created by merging are removed.

The complete inventory matches before/after: files598→591;1533 named test/benchmark/fuzz/example targets;2273 non-import declarations. Targets SHA256 af2da37c130a57ea78c734fef5e75dd816cad6a89ea5a6b1574b66457c38cf69; declarations SHA256 0db2db4770309f955d721163ab83c0a49cb5477977fdb8e9b387727510dad8d9. This comparison parses every file with object resolution/comments disabled, clears token positions, formats each declaration, then sorts package-qualified declarations/target names before hashing JSON arrays. It checks unchanged assertions/helpers as well as test discovery; excluded build configurations are not implied to have run.

Five post-move shuffled model/paid/code-mode repetitions pass26.514s; vet, Staticcheck, logging, register/source-hash and diff checks pass. The full pre-move runtime run passes durable423.520s/tools17.580s and all26 repository packages. No production code changes in the mechanical commit. Temporary merger/parser/inventory files are removed.
