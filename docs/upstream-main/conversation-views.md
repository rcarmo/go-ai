# Conversation views and transcript windows

`ConversationHandle.View` and `Watch` now expose transcript history with independent windows. Model-context APIs and legacy event snapshots retain their active-head entries and receipt pairing. The target is upstream `81f3cd412758ebdd92324c7d63b669a189408d0a`.

## Transcript-window checkpoint

Both methods accept one optional `ConversationWindow`. Its pointer fields distinguish omitted `Min`/`Max` from explicit invalid zero. Defaults are `min=50`, `max=2*min`; positive bounds must be safe integers with `max >= min`. Native DTO limits continue to apply to actual values. A large declared window can hydrate a short transcript without allocating its declared capacity. `From` is an inclusive entry ID used only at acquisition; later publications append even when their IDs are below that initial cursor. The resolved scalar bounds are detached from caller pointers.

Initial hydration selects entries from `From` when that selection has at most `max` entries; otherwise it selects the newest `min`. Without `From`, it selects the newest `min`. `Before` identifies the actual preceding visible entry, including allocator gaps, and is omitted at transcript beginning. A future `From` starts empty with the latest existing entry as its predecessor. Fork hydration honours inherited cutoffs.

Live publications append their entries in ID order. Summary/reset markers stay in the transcript. Windows grow to `max`; a publication exceeding it drops the oldest entries down to `min` and advances `Before`. Each watch owns its bounds and detached values. Trimming allocates a retained slice so dropped entries are not kept through its backing array. Model-context head selection is unchanged. `conversationSnapshot` explicitly uses the active-head helper to preserve legacy event receipt pairing.

### Test crosswalk

Source locations refer to `packages/durable/test/harness-view.test.ts` at the pinned revision.

| Source assertion | Native production-path checks |
| --- | --- |
| Summary/reset append, summary head retained; fork inherited history | `TestMainTranscriptHeadMarkersAppend`, `TestMainTranscriptWindowForkHistory`; ordinary summary append also checks the single append operation at index 3 |
| Newest `min`, defaults, short history and predecessor | `TestMainTranscriptDefaultHydration`, `TestMainTranscriptWindowHydration`, `TestMainTranscriptWindowShortBeginningAndDefaults` |
| Grow through `max`, then trim to `min`; independent observer windows | `TestMainTranscriptIndependentWindowGrowthAndTrim`; includes multi-entry publications and operation replay |
| Initial inclusive `from`, too-old fallback, future cursor, no predecessor | `TestMainTranscriptWindowHydration`, `TestMainTranscriptFromOnlyConstrainsAttachment`, `TestMainTranscriptWindowShortBeginningAndDefaults` |
| Reject invalid bounds, allow equal bounds | `TestMainTranscriptWindowRejectsInvalidOptions`, hydration's equal-bound case; fractional/NaN values cannot inhabit native integer fields |
| Valid large declaration over a short transcript | Source-derived `TestMainTranscriptWindowLargeDeclaration` guards against wrongly applying `MaxMembers` to requested bounds |
| Overflow replacement stays bounded | Native-boundary `TestMainTranscriptWindowNativeOverflowReplay`; source's 6,000-publication/10,000-operation scenario is **not** ported |

The first two history checks fail on all three backends against the preceding production overlay: 65 entries hydrate instead of 50, and a summary removes earlier transcript entries. An initial overlay probe missed the logical `/workspace` path and therefore ran current production; the corrected logical-and-physical mapping produces the six failures. A development bounds check also caught an over-restrictive `MaxMembers` cap on declarations; it is removed without changing actual DTO limits. The older reset-only watch assertion now checks all retained transcript entries while keeping replay, detachment and re-entry assertions.

Focused transcript/view/provider-identity/legacy-event normal checks pass three repetitions with shuffle 42 in CGO 0 and 1; focused race checks pass three repetitions with shuffle 42. Durable vet passes in both modes (Go 1.27.3, GOMAXPROCS=4). Disabling live trimming fails all three independent-window backend cases; ignoring initial `From` fails nine hydration leaves. Raw source extracts and overlay files are disposed after analysis. No full-suite, full-race, profiling or acceptance result is established by this checkpoint.

## Provider projection checkpoint (`56073ba`)

Native conversation views now include the singleton `pi.provider` document when it exists, matching the mounted built-in set in upstream `81f3cd412758ebdd92324c7d63b669a189408d0a`, `packages/durable/src/harness/view.ts` and `packages/durable/test/harness-view.test.ts`.

`mountedKind` had omitted it while admitting `pi.agent`, `pi.live`, `pi.inbox` and `pi.usage`. The shared filter now covers provider documents in initial `View`/watch hydration, live publications and overflow resnapshots. Retirement removes the mounted document. An absent provider document is not created by reading a view. Provider identity generation, persistence, requests, fork policy and credentials are unchanged.

Focused tests also found a replay defect for empty views. `buildConversationView` retained a nil entry slice, so overflow operations replaced the root with `entries: null`; `copyConversationView` exposed the same callback value as `entries: []`. Empty views now consistently expose an array, and root operations reproduce the detached callback value.

### Provider evidence

`durable/view_provider_main_test.go` covers hydration, creation after acquisition, live update, retirement, overflow resnapshot, detached baseline/callback values, empty-entry shape, operation replay and a callback re-entering `View`. Each mode runs on memory, journal and SQLite. Overflow tests consume the reset and any later queued frames in order; they do not assume an overflow discards subsequent publications. The earlier conversation-watch test now asserts four present built-ins, including the provider identity, instead of the obsolete three-document count.

Before the production changes, all nine new backend cases failed. Focused tests (`TestMainConversationViewProviderProjection|TestConversationWatch|TestProviderSessionIdentity`) pass three repetitions with shuffle 42 in CGO 0 and 1; focused race checks pass three repetitions with shuffle 42. Durable vet passes in both modes. Two independent logical-and-physical-path source overlays restore the provider omission or nil-entry shape; each fails all nine backend cases. Mutation artifacts are removed after analysis. No full-suite, full-race or pre-release profiling result is claimed.

## Remaining view work

Exact source overflow state and operations remain open. Native commit-subscription overflow rehydrates the newest `min`, discards the acquisition-only `From`, then applies later queued publications. It can retain fewer entries than an uninterrupted source window at the same final publication. Source advances each mounted window before asynchronous delivery; native watches advance during delivery. The next step is publication-time per-window state with bounded delivery, while preserving failure/Close and adapter authority. Native generic diffs replay values but can combine source append/trim operations into a different splice sequence.

Hydration currently derives visible history from the admitted snapshot; source uses bounded storage scans. Bounded scan/work parity, shared mounts/documents, reference identity and long-lived collection tests are open. Existing detached native values do not establish source shared-reference identity. Full `ConversationTranscript` gap/load/resume semantics and synchronous observers also need implementation/review.

Synchronous observers remain a separate [publication-contract gap](synchronous-observers.md). Both source test inventories remain explicitly partial.
