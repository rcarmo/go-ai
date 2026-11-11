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
