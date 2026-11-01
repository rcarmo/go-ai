## Durable cached storage and conversation tails

Pinned source: `81f3cd412758ebdd92324c7d63b669a189408d0a`. The twelve direct cases in `session-cached-storage.test.ts` and `session-conversation-tails.test.ts` have been reviewed against native storage/session interfaces. Their native ports are incomplete. The corpus disposition `reviewed-open` means assertions reviewed with unresolved native mapping; it does not count passing native cases.

### Source assertions and native gaps

| Source case | Required behaviour | Native mapping / remaining work |
| --- | --- | --- |
| Cache published writes | Published task writes answer reads without backend calls | Session adopts its private image after `core.apply`; source point-read/counting interface still needs adoption |
| Overlapping read/commit | A stale in-flight read cannot replace a newer published value | Native task reads and commits share the Session line; asynchronous reader/version behaviour needs the new storage interface |
| Live versus ended tasks | Retain 598 live tasks; bound ended-task cache at 256 | Native image is the backing state, not a separate terminal-task LRU; retention/resource mapping open |
| Receipt versus publication | Commit receipts alone do not update the cache | Native Commit combines settlement/adoption; review source group-commit publication/barriers before adding cache hooks |
| Per-record read versions | Invalidate only overlapping record reads; clear version tracking afterwards | No independently awaited point-read interface in the current native Session |
| Awaited terminal records | Pin results through the waiter's first resumed state; release on its next change or end | Map scheduler wait ownership and persistence before adding eviction |
| Frozen shared records | Preserve record identity and recursively freeze retained state | Native internal views stay private; public reads detach. External JS reference/freezing semantics need a disposition |
| Tail reads and commits | Retain first latest-entry/marker read, then update through publication | Native history reads use the adopted image/context ranges; dedicated point-read/cache mapping open |
| Roots versus forks | New roots are known empty; forks require read/write resolution | Native fork-aware history exists; cache initialisation assertions not ported |
| Marker cutoffs | A newest marker answers at/after its ID; older cutoff reads must not overwrite it | Native ancestry/cutoff history tests exist; cache-specific assertions not ported |
| Tail bounds | Bound latest IDs at 4096 conversations and markers at 256 | No separate native tail LRU; backing-state limits are not equivalent cache bounds |
| Frozen retained marker | Freeze the retained record without changing its identity | Private immutable-view/public-detach boundary applies; source reference semantics remain open |

`Storage` currently exposes snapshots, scans and bulk queries. `OpenSession` requires `nativeStorage`; raw source storage implementations and receipts do not map directly to it. A detached cache helper unused by production Session reads would not close these gaps. Next: source session/group-commit tests and the point-read/adoption boundary, preserving native adapter fault gates, ownership, cancellation and detached public records.

### SQLite internal read correction

The comparison exposed an avoidable copy in production: `Session.taskSnapshot` reused the private adopted image for exact memory/journal stores, but exact SQLite stores fell through to detached `Snapshot`. The new test fails on 1ef719c because SQLite copies that image. Exact SQLite now follows the same internal-view path. Wrapped SQLite adapters still call their Snapshot method, including injected failures. The Session/store lines still protect admission and the view; public Snapshot remains detached. No cache is added and no storage receipt/publication timing changes.

Three native tests cover adopted-image identity, public mutation isolation, visibility of a subsequent commit, wrapper calls/failures, cancelled admission and reads after close. These are native read-boundary checks; they do not replace the twelve source cache tests.

### Diagnostic measurements

Go 1.27.3, GOMAXPROCS=4, Linux/amd64, i7-12700; `BenchmarkMainSQLiteTaskSnapshot`, three one-second samples with CPU and allocation captures. Both versions create the same validated SQLite state: 64 entries, each with a 4096-byte body. The measured callback only checks entry count; setup is outside benchmark timing. This measures internal read cost, not full public Task latency.

| Metric | Before | After |
| --- | ---: | ---: |
| ns/op, three samples | 387318 / 465979 / 591939 | 174.5 / 139.4 / 126.0 |
| median ns/op | 465979 | 139.4 |
| B/op | 356393–356394 | 0 reported |
| allocations/op | 330 | 0 reported |

Median latency falls by about 465840 ns (99.970%, about 3343×). Even the fastest baseline versus slowest new sample exceeds 2200×; the baseline timing spread does not explain the gain. Unrelated image size no longer forces a detached clone on each internal SQLite task read. Public output detachment still has its normal cost.

Before, cumulative CPU attributed 47.65% of samples to `taskSnapshot`; bounded encoding consumed 49.70% profile-wide. Read-path allocation-space samples totalled about 4664 MB, dominated by bounded encoding, decoded JSON cloning and candidate tables. Allocation-object leaders were the same three functions. After, the read-path allocation focus matched no samples; remaining allocation samples came from validated fixture creation/commit and benchmark calibration. The remaining allocation capture totalled about 1.56 GB across fixture setup and calibration; calibration counts differ between versions, so this total is not a per-transaction comparison. The go-ai storage-adoption work must isolate a fixed 64-write setup workload and measure staged batch-validation/encoding before acceptance; no write-path improvement is established. Residual read CPU is dominated by admission locks/channels and failure checks; these preserve cancellation/ownership, and the measured median is 139.4 ns. No further read-path tuning was needed for this increment.

Raw CPU/heap captures and matching binaries were deleted immediately after comparison. The benchmark and concise conclusions remain. Full pre-release CPU/allocation profiling is incomplete.

### Checks and portability

Root normal tests, affected race checks, root vet, module verification and a CGO-disabled durable package build pass. Dependencies, accepted assets and v1.4.0 are unchanged. The new exact-type helper has CGO/non-CGO implementations; wrappers cannot inherit permission to bypass Snapshot.

At this increment, CGO-disabled durable tests could not compile because three untagged files referenced SQLite-only APIs. The subsequent [test portability correction](durable-cgo-tests.md) isolates only SQLite helpers and verifies all generic cases plus full root normal/vet in both configurations. The full durable race suite gate remains open: even the later 420-second suite attempt timed out; affected tests passing does not close it.
