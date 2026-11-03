# Cutoff ordering and generation salvage

The memory/journal/SQLite matrix below records historical verification before Rui's SQLite-only correction. [Runtime f284e25](sqlite-only.md) removes Memory and Journal, and these four functions also pass its SQLite-only selection.

Runtime `fea6d8ef444a30e81cddb19c3c0c9114b2de14a8` faults ordinary cut-off tasks before taking a registry snapshot, cancels deferred responses retained by retry-of-poll checkpoints at the limit, and preserves the source's generation phase/count text. Base: `b949d234d121501af88786d9fb7db7a75608d089`. Target: official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`.

## Pinned assertions

`durable/harness_cutoff_salvage_main_test.go` adds four focused functions:

* `KeepsCommittedPartialAtLimit` translates `harness-restart-failure.test.ts:621`: real request streaming, committed progress, failed-host reopen at limit1, reopened partial, unanswered/restart_limit settlement with exact detail, failure ownership, user/aborted-assistant entries with retained text, and no replacement model call.
* `DropsPartialPastLimit` translates source648. It retains that source's explicit raw-storage `restarts=2` seed after the real stream crash, checks the third restart on reopen, exact repeated-interruption detail, user-only entries and cleared generation. No task outcomes or live ownership are fabricated.
* `BeforeRegistrySnapshotDerived` follows `scheduler.ts:1933–1955`. The native registry has no snapshot callback; holding its mutex exposes premature snapshot access. A seeded ordinary task at the limit must fault while that mutex is held, without running its definition. A one-second external select releases the lock and joins the waiter on failure.
* `DeferredSalvagePhasesDerived` follows `generation.ts:546–583` (`endCutOff`/`deferredOf`). After a real request owns the run, native codec seeds cover poll and retry-of-poll at/beyond the limit. The supported legacy checkpoint partial adapter preserves the request's text through these phase seeds. At the limit cancellation runs once and the partial becomes aborted; beyond it neither occurs. Cancellation receives the original handle; failed endings and phase/count text remain exact.

Native IDs, request checkpoint `intent`→source `request`, message-entry roles and real modernc SQLite replace the source wrappers. The native live document's extra `failedRun=true` compatibility flag is asserted explicitly alongside `runFailure`; no other extra fields are accepted. The failed-Session/cancel/join/reopen fixture replaces process death. The original source648 restart seed and the derived legacy partial/phase seeds remain separate.

An initial arbitrary poll/retry phase rewrite dropped modern request presentation, without executing a real provider transition. Later [poll-boundary checks](poll-boundary.md) establish that f9's poll API returns one terminal message and exposes no streaming progress to the harness. Native provider deltas leave no partial; real interrupted-fetch and cancellation-cutoff checks pass without runtime changes. The seeded legacy partials above supply no streamed-poll API guarantee.

## Baseline and changes

With corrected fixtures, unchanged production passed the at-limit request-partial function and failed the other three: **15 backend leaves, 3.951s, no skips/global timeout**. The failures isolate eager registry access, missing `in a row`, retry reported as request, and omitted retry-of-poll cancellation. Existing request partial salvage/drop logic needed no change.

Initial probe errors receive no runtime-failure credit: a physical-path-only overlay selected zero tests; zero encoding budgets and native optional/numeric/live-shape assumptions were corrected; a synchronous lock waiter caused a75s test timeout. The final external-select waiter releases the registry before joining. Initial poll/retry phase seeds lost modern presentation; the final derived controls explicitly use the native legacy partial adapter.

`task_scheduler.go` now takes one lazy snapshot after the cutoff branch. Ordinary cutoff reports use task identity/reason without resolving an installer, matching source fault reporting before snapshot resolution. Other candidates retain their normal snapshot, definition, migration and invocation path. Generation messages keep retry as retry and add `in a row` for repeated interruptions without changing the shared tool-count formatter. `cancelDeferred` accepts retry checkpoints that retain a handle; ordinary request retries still have no handle and return immediately. Callbacks remain outside the Session line with existing credential/cancellation/reporting fences.

## Verification

Commands source `scripts/project-env.sh` and use Go1.27.3, `CGO_ENABLED=0`, `GOMAXPROCS=4`, `GOTOOLCHAIN=go1.27.3` and the owned `runs/cutoff-order-salvage/20261103` hierarchy. Logical and physical overlays exclude only the protected continuation prototype.

* Four new functions pass after fixes:1.018s.
* Promoted ordinary tests plus four affected controls: **8 functions/count3/shuffle42,24 passes,0 failures/skips,5.756s**. Related controls cover missing-definition cutoff, deferred abort success/error/panic, poll faults and interrupted-poll retry edges. These existing controls receive regression credit within this selection; their broader legacy contracts remain unaudited.
* The tightened five-field run-failure shape passes the four new functions once more:4 passes/0.953s; vet/Staticcheck pass again.
* Seven effective logical/physical fault overlays reject the expected27 backend leaves: eager snapshot3, drop at limit3, keep past limit3, skip retry cancellation3, cancel past limit6, omit count qualifier3, retry-as-request6. No build errors, skips or global timeouts receive credit.
* Durable vet and Staticcheck v0.8.2 with tests pass; all1460 Go files match the validated static copy. Formatting/diff and protected-prototype comparisons pass.

The completed39/81-check gates and recovery baselines were not rerun. No full suite, profiling, CGO/race, TypeScript/Vitest, hosted CI, release or asset publication ran. Dependencies, QuickJS/wazero and immutable nativev1.4 assets are unchanged. One main test-crosswalk and matching review evidence cell change; dispositions/flags and the362/210 corpora/428-path inventory stay unchanged.

After analysis,28MB of source extracts/static copies/mutation/log scratch and1.3GB of Go build cache were removed with no active Go/compiler/static jobs. The prototype, source mirror, dependencies, toolchains and intentional assets are preserved.

## Next controls

The [poll-boundary checks](poll-boundary.md) cover terminal-only polling, real abort-handler cutoff and repeated host loss during deferred cancellation. Failure/report ordering beyond those selected assertions and complete phase DTO/ownership behaviour need further oracles. Registry locking here tests the native snapshot dependency; full dynamic registry semantics need further source reconciliation. Whole-file restart-failure review stays `pending-review`; full v1.5.0 alignment is unverified.
