# Context head rewind and off-line reads

Native context reads now derive and detach outside the Session line, then re-enter to check health and cache-installation witnesses. Commits remain serial and grouping stays disabled. `Session.ContextView`, `ConversationHandle.ContextView` and `ConversationHandle.Context` share this path; the model-context API captures its agent document from the same admitted image as the transcript.

## Pinned source

Reviewed at `81f3cd412758ebdd92324c7d63b669a189408d0a`:

- `packages/durable/src/harness/context.ts:24-156`: capture bounds on the Session line, scan immutable entries through the captured tail off the line, and recheck currentness before retaining a range.
- `context.ts:184-197`: `advanceRange` leaves non-current ranges unchanged and drops a range when a new head moves its start backwards.
- `packages/durable/test/harness-context-range.test.ts:8-46`: pause an off-line scan, publish `missed`, finish the old scan, then publish `last`. The old range must be non-current, and the next read must include all three entries.
- `packages/durable/test/harness-context.test.ts:394-425`: historical context matches a fork at the same cutoff; later edits and resets do not affect earlier cutoffs. The source case also checks synthesized missing tool results, which this new test does not exercise.

## Head rewind regression at 90a258b

`TestContextMainHeadRewindRestoresDiscardedHistory` in `durable/context_rewind_main_test.go` covers memory, journal and SQLite. That test-only checkpoint changed no production code.

The test first retains a forward-head range and verifies that its history has discarded the first entry. It then moves the head backwards and checks literal entry IDs and message text, without using native derivation as the expected result. It checks a later replacement edit, historical reads on both sides of the heads and edit, retained-current-range state, detached output mutation and a subsequent append. Six historical cutoffs match independently created conversation forks; later invisible cutoffs are rejected.

A disposable overlay constrained fresh scans to the previous range's retained start. All three backends failed at the backwards-head assertion because the first entry was missing. The initial overlay used only the physical checkout path and did not affect the logical Go package path; the corrected mapping included both paths. The corrected failing run is the mutation evidence. Overlay files were removed after diagnosis.

Validation with Go 1.27.3 and GOMAXPROCS=4:

- New rewind, existing cache and baseline controls: ten shuffled repetitions, seed 42, CGO 0 and 1.
- Context/cache/retention controls: race, three repetitions each, seeds 7 and 42, CGO 1.
- Full root normal tests, vet and pinned staticcheck v0.8.2: CGO 0 and 1. Module verification passed.

The full durable race at `08a2973` preceded this test-only checkpoint. The later off-line implementation below has a fresh full race result.

## Native capture, derivation and installation

`durable/context_read.go` captures an admitted immutable published image and a copy of the retained range header under the Session line. Immutable history/view payloads can be shared; sequence, currentness and retention fields cannot. `deriveContextRange` and `extendContextRange` take explicit limits and read no live Session state. Derivation and deep detachment run after line release.

Installation re-enters the line and repeats native core health admission. The published sequence, a process-local retention revision and the retained slot identity must still match. Every retention settlement advances the revision, including empty-cache settlements, so a settings or host transition invalidates pending cold reads too. Installation settles expiry again before replacing a slot and preserves an existing idle timestamp. A stale result can describe its captured bounds, but its candidate is discarded instead of becoming a current cache. The next read extends from the published image and includes intervening entries.

These are conservative global witnesses: unrelated publications and retention settlements can also prevent installation. The reader does not retry. Raw stored-but-unpublished entries leave the published witness unchanged; actual private receipt adoption invalidates it even without ordinary publication bookkeeping.

Wrapped reads retain their serial Snapshot boundary. Every read derives from the wrapper's current data, including changes at an unchanged sequence; no native range is reused or installed. Adapter request errors retain the existing `StorageRequestError` projection. Native empty views retain their nil fields, and invalid bounds/nil contexts remain request rejections.

## Off-line validation

`durable/context_read_main_test.go` uses the production capture/derive/install stages and all three public entry points. Request-owned `Context.Err` barriers pause before derivation and after detachment. A concurrent public Commit completes before release; the captured view still contains only `first`, stale cache installation is rejected, and a later read includes `first`, `missed`, `last`. No production test hook or sleep-based ordering is used. These tests depend on the documented admission/checkpoint call order.

Other controls cover cold/warm caches, a newer read winning installation, retained idle timestamps, expiry without timer delivery, settings changes back to the original value with an empty cache, cancellation, explicit Close, native storage Close, poison, first fatal failure, stored-versus-receipted private images, authoritative wrapper changes/errors at equal sequence, concurrent header access/detached mutation, and empty/invalid reads.

Four disposable overlays each failed across memory, journal and SQLite:

- Removing the published-sequence guard permits an old read to overwrite a privately adopted receipt's cache state. Ordinary publication also advances the retention revision, so removing sequence alone did not fail that redundant-witness case; private receipt adoption isolates the sequence guard.
- Removing the retention-revision guard resurrects a cold range after settings changed to zero and back.
- Moving derivation back onto the Session line blocks the public Commit at the request barrier.
- Restoring sequence-only wrapper caching returns an earlier projection after the wrapper changes its data.

Final Go 1.27.3/GOMAXPROCS=4 checks:

- New reads plus context cache/baseline/head/retention/timer controls: normal three repetitions, seed 42, CGO 0 and 1; race three repetitions each, seeds 7 and 42, CGO 1.
- Full root normal tests and vet: CGO 0 and 1. Pinned staticcheck v0.8.2 and module verification pass. The deliberate nil-context rejection has one statement-local SA1012 exception; the first staticcheck run caught the missing annotation.
- Full durable CGO-1 race, count 1, timeout 1200s: 661.818s, 4,324 named passes, zero failures and zero unfinished tests. This includes 108 named passes under the new context-read tests. Seven exact-worker Codemode process cases skip without the opt-in installation; the integration gate is unchanged.

JSON test events were counted in a streaming parser without retaining a raw log. Mutation overlays and owned empty credential homes were removed after use. No extensive profiling or performance comparison was run under Rui's current direction. The full race workload differs from earlier checkpoints, so suite times are not a performance comparison.

## Remaining boundaries

The source range-race assertion now has native value/currentness and real public-overlap controls. Source storage page/scan work bounds and incremental commit-time `advanceRange` work are not established: native derivation still selects history from a captured Snapshot and guards installation globally. Ordered expiry, task-root lifetime pins, inactive Wake, grouped writer/publication/failure/continuation contracts and broader semantic convergence remain open. No public API, dependency, storage format, release ref or accepted asset changed.
