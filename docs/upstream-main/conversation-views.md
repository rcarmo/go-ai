# Conversation-view built-in documents

Native conversation views now include the singleton `pi.provider` document when it exists, matching the mounted built-in set in upstream `81f3cd412758ebdd92324c7d63b669a189408d0a`, `packages/durable/src/harness/view.ts` and `packages/durable/test/harness-view.test.ts`.

`mountedKind` had omitted it while admitting `pi.agent`, `pi.live`, `pi.inbox` and `pi.usage`. The shared filter now covers provider documents in initial `View`/watch hydration, live publications and overflow resnapshots. Retirement removes the mounted document. An absent provider document is not created by reading a view. Provider identity generation, persistence, requests, fork policy and credentials are unchanged.

Focused tests also found a replay defect for empty views. `buildConversationView` retained a nil entry slice, so overflow operations replaced the root with `entries: null`; `copyConversationView` exposed the same callback value as `entries: []`. Empty views now consistently expose an array, and root operations reproduce the detached callback value.

## Evidence

`durable/view_provider_main_test.go` covers hydration, creation after acquisition, live update, retirement, overflow resnapshot, detached baseline/callback values, empty-entry shape, operation replay and a callback re-entering `View`. Each mode runs on memory, journal and SQLite. Overflow tests consume the reset and any later queued frames in order; they do not assume an overflow discards subsequent publications. The earlier conversation-watch test now asserts four present built-ins, including the provider identity, instead of the obsolete three-document count.

Before the production changes, all nine new backend cases failed. Focused tests (`TestMainConversationViewProviderProjection|TestConversationWatch|TestProviderSessionIdentity`) pass three repetitions with shuffle 42 in CGO 0 and 1; focused race checks pass three repetitions with shuffle 42. Durable vet passes in both modes. Two independent logical-and-physical-path source overlays restore the provider omission or nil-entry shape; each fails all nine backend cases. Mutation artifacts are removed after analysis. No full-suite, full-race or pre-release profiling result is claimed.

## Remaining view work

This fixes built-in projection and empty-array shape only. The native view still exposes active-head context history. Upstream views expose transcript windows with `min`/`max` bounds (defaults 50/100), an initial `from` cursor and a `before` predecessor, and append summary/reset entries without discarding earlier transcript entries. Per-observer windows, mounted-document sharing and full `ConversationTranscript` gap/load/resume semantics remain unimplemented or unreviewed. Existing detached native values do not establish upstream shared-reference identity.

Synchronous observers remain a separate [publication-contract gap](synchronous-observers.md). Both source test inventories remain explicitly partial.
