## Published idle membership

`Session.contextBusy` now reads cached conversation/global counts from the Session's published metadata. Warm queries need one scope lookup and no task traversal. Raw writers, candidate images and wrapped public Snapshot projections keep their existing admission and data contracts. Session publication stays serial; grouping and overlap are disabled.

`durable/idle_index.go` stores one node per task and conversation, plus global scope 0. Task-owned children attach to task nodes; conversation-owned tasks attach to conversation nodes. Ownerless conversations attach to global 0. Background tasks cut their upward edge, while their owned conversations keep separate busy counts. Ended owners remain connectors. Space grows with record count, without storing each task's complete ancestor set.

Each node carries live and idle-relevant descendant counts. A held failed-input placeholder contributes to its parent's live descendants even when the placeholder itself is idle. This matches `heldFailedInput` and `ownedLive`, including terminal records whose actual hosts have not returned. Live-document changes refresh eligible queued generations in the affected conversations; terminal history is excluded from that fanout.

The first query builds from the immutable published image. Receipted publication applies final rows from the write list before retention settlement. Entry-only publication advances the sequence witness without visiting nodes. Reservation, rejected reservation and actual host retirement update local weights on the Session line. Close releases the index after host joins. Private receipt adoption without the production write list triggers a rebuild from the published image.

Changed ownership edges detach before any new edge attaches. Valid legacy reparenting therefore cannot create a temporary cycle or double-count a moved subtree. Accepted legacy task/conversation cycles use the original visited-set scan. Conflicting `failedRun` flags on multiple live documents also use the original scan, preserving its first-match map semantics. Retiring the conflicting document restores indexed queries.

## Checks

`idle_model_main_test.go` checks all conversation/global values against an independent downward graph across the pinned 200-step workload. Every warm busy query must leave the node-work counter unchanged, with one initial build across all steps. Target enumeration still uses `idleTargets`; its work bounds need separate implementation.

`idle_index_main_test.go` adds six cases across memory, journal and SQLite: held children/document deltas, physical terminal hosts/Close, entry-only publication/private receipts, legacy reparenting, legacy cycles and conflicting live documents. Mutations removing background detachment, confusing live with idle children, ignoring physical hosts or skipping publication updates fail all three backends. Mutation overlays and logs were removed after analysis.

Focused normal runs pass three repetitions with shuffle 42 in both CGO modes; focused race runs pass three repetitions with shuffle 7, including failed-run, ownership and Session controls. Fresh full root tests, vet, staticcheck v0.8.2 and module verification pass in both CGO modes. The full durable race run passes in 655.742 seconds: 4,201 named passes, zero failures, zero unfinished tests and seven opt-in Codemode process skips. Events, status and empty homes were removed after accounting. Workloads differ from the preceding checkpoint, so suite times are not a performance comparison.

An earlier root CGO-disabled run failed unchanged `TestBedrockRawResponseHeadersAndEventHookWireMain/success`: two raw events, no done event, `decode event: read tcp ...: use of closed network connection`. The cause is unconfirmed. It did not reproduce in 100 isolated normal runs with shuffle 42 or 30 full Bedrock-package race runs with shuffle 7. The later root passes do not resolve that failure; Bedrock stream-lifetime diagnosis is an open release follow-up.

## Query measurements

Equivalent 4,096-terminal-task fixtures with a real host and future sleeper, Go 1.27.3, linux/amd64, GOMAXPROCS=4 and CGO enabled. Baseline uses the prior `contextBusy` through a disposable overlay; the final case uses the index. Three two-second samples per query:

| Query | Baseline ns/op | Indexed ns/op | Median change |
| --- | --- | --- | --- |
| Idle conversation | 82383 / 80270 / 78611 | 21.75 / 21.56 / 21.20 | -80248 ns / -99.973% |
| Busy conversation | 36919 / 24876 / 31749 | 21.19 / 20.92 / 21.17 | -31728 ns / -99.933% |

Both stages use zero bytes and zero allocations per warm query. A separate cold-build benchmark gives 1119910 / 1093191 / 998545 ns/op, 699400 allocated bytes and 4153 allocations per build. Retained heap size was not measured. The median cold cost equals about 14 prior idle queries or 35 prior busy queries in this fixture; long-lived caches amortise that cost.

CPU, alloc_space and alloc_objects profiles were inspected for both query stages. Baseline `contextBusy` accounts for 9720 ms cumulative of 11220 ms sampled CPU; indexed scope map lookup accounts for 8530 ms of 11020 ms. The indexed workload executes far more queries, so these cumulative totals are not per-operation comparisons. Allocation profiles are dominated by fixture construction and strict store preparation. Cold build and publication costs need broader release profiling. All captures, matching binaries, overlays and disposable logs were removed after comparison.

Ordered expiry inspection, target enumeration, internal task-root pin/lifetime equivalence, inactive Wake work bounds and full upstream semantic acceptance need further work. The private build/work counters measure this index; they do not replace upstream's complete `fullIdleChecks` diagnostic contract.
