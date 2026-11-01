# Conversation-view assertions at 81f3cd41

`packages/durable/test/harness-view.test.ts` contains 23 tests at pinned commit `81f3cd412758ebdd92324c7d63b669a189408d0a`. The table distinguishes translated assertions from related native regressions. Candidate files have not established complete assertion equivalence.

| Source line / test | Native evidence | Remaining work |
| --- | --- | --- |
| 113: hydrate newest entries/built-ins | Candidate: `view_provider_main_test.go`, `view_test.go` | Exact provider/chat inputs and every source assertion |
| 128: one frame per touching commit/replay | Candidate: `view_test.go` | Natural frame count and exact input sequence; callback barriers do not establish JS ordering |
| 160: structural sharing/unmounted silence | `view_lifecycle_oracle_main_test.go` checks unmounted silence from the separate atomic test | Exact source sequence; shared reference assertions |
| 178: summary/reset head markers | `TestMainViewUpstreamHistoryOracles/summary-reset`: exact a/b/c/summary/d/reset sequence, natural three frame arrays, literal summary splice/head, replay/committed equality | Native setup/required Value/ID adaptations; see history oracles |
| 204: inherited fork entries/head markers | `/fork-markers`: exact a/b/fork-at-b/summary, initial ordered IDs, first frame kinds/committed equality | Native setup and ownerless Fork API adaptations |
| 219: fork observes only own commits | `/fork-isolation`: first/fork/parent/child, initial kinds/parent cutoff, exactly one child frame | Flat native Parent/ParentAt representation; no JS timing equivalence |
| 234: retirement/recreation mounts whole | `TestMainViewUpstreamMountOracles/retire-recreate`: separate retire/recreate commits, exact delete/whole-set tools:[] tuples, first frame absent | Native explicit document creation adapts lazy tx.doc; setup differs |
| 251: fold undelivered commits | `/undelivered-101`: exact 101 kind:note commits before Start, one call/non-root/replay/committed newest-50 equality | Native setup/Value/IDs; broader threshold domains remain open |
| 274: independent watches/remount | `/independent-remount`: one/two, peer Stop/single frame, surviving delivery, last Stop/fresh watch value equality | Source rebuilt reference inequality and internal shared-mount equivalence remain unported |
| 295: close/late acquisition | Candidate: `observation_test.go` | Exact conversation-level acquisition/end diagnostics |
| 304: queued cancel/close acquisition | Candidate: `view_publication_main_test.go` | Exact source queued acquisition scenarios/error assertions |
| 330: shared mount/failing listener | `TestMainViewUpstreamListenerIsolation` | Shared docs identity; source provider/chat setup adapted |
| 351: atomic multi-entry/document commit | `TestMainViewUpstreamAtomicPublication` | Native empty Value envelope/minted IDs/setup adaptations documented |
| 398: initial newest min/default window | `TestMainViewUpstreamHydrationOracles/newest-min-and-default` | Setup/ID adaptations documented; no identity claim |
| 412: grow to max, splice to min | `TestMainViewUpstreamAdvanceOracles/grow-splice`: exact batches, natural lengths `[4,5,3]`, literal operations/predecessor/replay | Shared entry identity; native setup/Entry adaptations |
| 437: oversized commit keeps newest min | `/burst`: two notes, five burst entries in one commit, one frame/newest two/predecessor/replay | Native setup/Entry adaptations |
| 455: from/exact/short/future/overflow/start | `TestMainViewUpstreamHydrationOracles/from-exact-short-future-overflow-start` | Setup/ID adaptations documented |
| 482: resumed window advances | `/resumed`: five notes/from newest+1, two notes, final IDs/predecessor/every-frame replay | Native setup/Entry adaptations |
| 495: from applies only initially | `/from-only-initial`: three notes/from newest+1000, empty initial array, full next record below from and predecessor | Native setup/Entry adaptations |
| 512: invalid/equal bounds | Candidate: `view_window_bounds_main_test.go` | Integer API domain and RangeError/error diagnostics disposition |
| 525: per-observer window/shared documents | Candidate: `view_window_main_test.go` | Exact source note sequence plus shared-doc/entry identities |
| 548: bounded overflow root | Candidate: `view_pending_bounds_main_test.go`, `view_publication_main_test.go` | Exact 6,000-commit source threshold/input sequence; preserve native 4,096-op/DTO limits |
| 570: dropped entry collection | `TestMainViewUpstreamDroppedEntryLifetime` | Native backing-array adaptation below; JS EntryRecord identity/GC is not directly represented |

## Dropped-entry lifetime adaptation

The source uses SQLite, a live watch with `min:5,max:10`, one note followed by 200 more notes, at most ten visible entries and at most three GC rounds. `durable/view_gc_oracle_main_test.go` preserves those inputs and bounds. Memory/journal runs are additional native checks.

Go Entry is a struct stored in slices. The native weak pointers target the actual pointerful backing arrays at the first entry, both in the detached callback value and the watch's private current/head windows. Keeping those private targets prevents detached callback copies from hiding retained internal windows. This does not reproduce individual JavaScript EntryRecord object identity, per-entry collection while other elements share a Go allocation, or structural sharing between revisions.

A no-inline allocator/callback helper leaves the collection assertion without strong entry pointers. The native positive control retains the same three targets across three GCs, then releases them and uses the source's three-round collection bound. A separate scenario never retains them. The watch, Harness and backend stay live throughout; no finalizer, tiny scalar proxy, skipped assertion or closed-backend collection substitutes for the targets. Public weak-pointer semantics do not guarantee eventual collection on every Go runtime; these development checks use Go 1.27.3.

Unchanged production passes three normal repetitions/shuffle 42 in CGO 0/1 (4.509s/8.121s), three race repetitions/shuffle 7 (23.732s), and combined lifetime/hydration/lifecycle race repetitions/shuffle 42 (25.655s). Durable vet/staticcheck v0.8.2 pass both modes; module verification passes. Retaining callback entry arrays and retaining old private heads each fail all six CGO-enabled backend/control leaves. Mutation files/logs and temporary source extracts were removed after analysis. No performance comparison, full-suite/full-race run or acceptance profiling was performed.

The separate retained WatchFrame.Context payload gap remains open; see [watch lifecycle oracles](watch-lifecycle-oracles.md).

See [mount oracles](mount-oracles.md) for retirement/recreation, undelivered folding and independent-watch/remount inputs, API adaptations and development checks at lines 234, 251 and 274.

See [history oracles](history-oracles.md) for exact summary/reset and fork inputs, assertions and development checks at lines 178, 204 and 219.

See [window advancement oracles](window-advance-oracles.md) for exact inputs, assertions, adaptations and development checks for lines 412, 437, 482 and 495.
