# Durable test consolidation

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
