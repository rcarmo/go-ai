## Published idle membership

`Session.contextBusy` now reads cached conversation/global counts from the Session's published metadata. Warm queries need one scope lookup and no task traversal. Raw writers, candidate images and wrapped public Snapshot projections keep their existing admission and data contracts. Session publication stays serial; grouping and overlap are disabled.

`durable/idle_index.go` stores one node per task and conversation, plus global scope 0. Task-owned children attach to task nodes; conversation-owned tasks attach to conversation nodes. Ownerless conversations attach to global 0. Background tasks cut their upward edge, while their owned conversations keep separate busy counts. Ended owners remain connectors. Space grows with record count, without storing each task's complete ancestor set.

Each node carries live and idle-relevant descendant counts. A held failed-input placeholder contributes to its parent's live descendants even when the placeholder itself is idle. This matches `heldFailedInput` and `ownedLive`, including terminal records whose actual hosts have not returned. Live-document changes refresh eligible queued generations in the affected conversations; terminal history is excluded from that fanout.

The first query builds from the immutable published image. Receipted publication applies final rows from the write list before retention settlement. Entry-only publication advances the sequence witness without visiting nodes. Reservation, rejected reservation and actual host retirement update local weights on the Session line. Close releases the index after host joins. Private receipt adoption without the production write list triggers a rebuild from the published image.

Changed ownership edges detach before any new edge attaches. Valid legacy reparenting therefore cannot create a temporary cycle or double-count a moved subtree. Accepted legacy task/conversation cycles use the original visited-set scan. Conflicting `failedRun` flags on multiple live documents also use the original scan, preserving its first-match map semantics. Retiring the conflicting document restores indexed queries.

## Checks

`idle_model_main_test.go` checks all conversation/global values against an independent downward graph across the pinned 200-step workload. Every warm busy query must leave the node-work counter unchanged, with one initial build across all steps. Indexed target sets are checked against the same graph at every step. Native admitted reads and receipted publication use the indexed path; wrapped and private candidate views retain `idleTargets`.

`idle_index_main_test.go` adds six cases across memory, journal and SQLite: held children/document deltas, physical terminal hosts/Close, entry-only publication/private receipts, legacy reparenting, legacy cycles and conflicting live documents. Mutations removing background detachment, confusing live with idle children, ignoring physical hosts or skipping publication updates fail all three backends. Mutation overlays and logs were removed after analysis.

Focused normal runs pass three repetitions with shuffle 42 in both CGO modes; focused race runs pass three repetitions with shuffle 7, including failed-run, ownership and Session controls. Fresh full root tests, vet, staticcheck v0.8.2 and module verification pass in both CGO modes. After allocation tuning, target-specific race tests also pass three repetitions with shuffle 42. The full durable race run passes in 670.896 seconds: 4,212 named passes, zero failures, zero unfinished tests and seven opt-in Codemode process skips. Events, status and empty homes were removed after accounting. Workloads differ from the preceding checkpoint, so suite times are not a performance comparison.

An earlier root CGO-disabled run failed unchanged `TestBedrockRawResponseHeadersAndEventHookWireMain/success`: two raw events, no done event, `decode event: read tcp ...: use of closed network connection`. The cause is unconfirmed. It did not reproduce in 100 isolated normal runs with shuffle 42 or 30 full Bedrock-package race runs with shuffle 7. The later root passes do not resolve that failure; Bedrock stream-lifetime diagnosis is an open release follow-up.

## Target enumeration

Each node retains only children with nonzero idle-relevant weight. Iterative traversal follows those active branches, emits tasks whose own weight is nonzero and sorts detached result IDs. Ended owners connect live descendants without becoming targets themselves. Empty child maps are released. Traversal space grows with its frontier; a 16-pointer local buffer handles small scopes without an extra heap allocation.

`taskSnapshotView` supplies an explicit published-image witness after native read admission. Wrapped Snapshot results carry no witness, even with matching sequence and high-water fields. Public idle waits and bound-wait refresh on actual release use that witness. Receipted native publication has its own known published image; rejected provisional transactions keep the original candidate scan. No Snapshot field comparison grants trust.

`idle_targets_main_test.go` checks four visited nodes with 4,096 terminal records and two live targets, caller mutation isolation, equal-sequence candidate records, authoritative adapter data/errors through public `WaitForIdle`, and 64-wide/64-deep trees across all backends. The wide tree exceeds the local frontier buffer. The deep tree emits only its live leaf. Held queues, terminal hosts, reparenting and fallback cases also compare indexed enumeration with the original predicate.

Three mutations fail: ignoring the read witness bypasses candidate/adapter data; emitting every nonzero task node includes ended-owner connectors; omitting active-child maintenance loses the model's first live target. Overlays and logs were removed after analysis.

## Membership query measurements

Equivalent 4,096-terminal-task fixtures with a real host and future sleeper, Go 1.27.3, linux/amd64, GOMAXPROCS=4 and CGO enabled. Baseline uses the prior `contextBusy` through a disposable overlay; the final case uses the index. Three two-second samples per query:

| Query | Baseline ns/op | Indexed ns/op | Median change |
| --- | --- | --- | --- |
| Idle conversation | 82383 / 80270 / 78611 | 21.75 / 21.56 / 21.20 | -80248 ns / -99.973% |
| Busy conversation | 36919 / 24876 / 31749 | 21.19 / 20.92 / 21.17 | -31728 ns / -99.933% |

Both stages use zero bytes and zero allocations per warm query. A separate cold-build benchmark gives 1119910 / 1093191 / 998545 ns/op, 699400 allocated bytes and 4153 allocations per build. Retained heap size was not measured. The median cold cost equals about 14 prior idle queries or 35 prior busy queries in this fixture; long-lived caches amortise that cost.

CPU, alloc_space and alloc_objects profiles were inspected for both query stages. Baseline `contextBusy` accounts for 9720 ms cumulative of 11220 ms sampled CPU; indexed scope map lookup accounts for 8530 ms of 11020 ms. The indexed workload executes far more queries, so these cumulative totals are not per-operation comparisons. Allocation profiles are dominated by fixture construction and strict store preparation. Cold build and publication costs need broader release profiling. All captures, matching binaries, overlays and disposable logs were removed after comparison.

## Target query measurements

The same 4,096-history fixture returns two sorted target IDs. Three two-second samples with Go 1.27.3, linux/amd64, GOMAXPROCS=4 and CGO enabled compare the prior scan/forest through a disposable overlay with active-child enumeration:

| Stage | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| Prior scan | 406187 / 399634 / 400743 | 41032 | 4 |
| Initial traversal | 156.4 / 154.4 / 155.1 | 40 | 3 |
| Exact-size output/local frontier | 116.7 / 116.0 / 119.4 | 16 | 1 |

The final median saves 400626 ns (-99.971%), 41016 bytes (-99.961%) and three allocations per query. CPU, alloc_space and alloc_objects profiles were inspected at all three stages. The initial allocation outlier was result growth plus frontier growth; preallocation and the local buffer remove those allocations. The final query allocates only its caller-owned two-ID result. Baseline CPU spends 83.92% cumulative in `idleTargets`; the final traversal accounts for 79.40% of its much larger query count. Aggregate profile totals are not per-operation comparisons.

Cold construction was also compared with the prior forest: median 977771 ns / 699416 B / 4153 allocations against 981282 ns / 699800 B / 4157 allocations. The median CPU difference is +3511 ns (+0.36%) with overlapping sample ranges; +384 B (+0.055%) and four allocations fund the active branch sets. Retained heap size and whole-publication cost were not measured. All captures, matching binaries, overlays and disposable logs were removed after comparison.

Ordered expiry inspection, internal task-root pin/lifetime equivalence, inactive Wake work bounds and full upstream semantic acceptance need further work. The private build/work counters measure this index; they do not replace upstream's complete `fullIdleChecks` diagnostic contract.
