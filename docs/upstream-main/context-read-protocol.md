# Context head rewind and off-line reads

Native cached context reads recover discarded history after a backwards head. `TestContextMainHeadRewindRestoresDiscardedHistory` in `durable/context_rewind_main_test.go` covers memory, journal and SQLite without changing production code.

## Pinned source

Reviewed at `81f3cd412758ebdd92324c7d63b669a189408d0a`:

- `packages/durable/src/harness/context.ts:24-156`: capture bounds on the Session line, scan immutable entries through the captured tail off the line, and recheck currentness before retaining a range.
- `context.ts:184-197`: `advanceRange` leaves non-current ranges unchanged and drops a range when a new head moves its start backwards.
- `packages/durable/test/harness-context-range.test.ts:8-46`: pause an off-line scan, publish `missed`, finish the old scan, then publish `last`. The old range must be non-current, and the next read must include all three entries.
- `packages/durable/test/harness-context.test.ts:394-425`: historical context matches a fork at the same cutoff; later edits and resets do not affect earlier cutoffs. The source case also checks synthesized missing tool results, which this new test does not exercise.

## Native regression

The test first retains a forward-head range and verifies that its history has discarded the first entry. It then moves the head backwards and checks literal entry IDs and message text, without using native derivation as the expected result. It checks a later replacement edit, historical reads on both sides of the heads and edit, retained-current-range state, detached output mutation and a subsequent append. Six historical cutoffs match independently created conversation forks; later invisible cutoffs are rejected.

A disposable overlay constrained fresh scans to the previous range's retained start. All three backends failed at the backwards-head assertion because the first entry was missing. The initial overlay used only the physical checkout path and did not affect the logical Go package path; the corrected mapping included both paths. The corrected failing run is the mutation evidence. Overlay files were removed after diagnosis.

Validation with Go 1.27.3 and GOMAXPROCS=4:

- New rewind, existing cache and baseline controls: ten shuffled repetitions, seed 42, CGO 0 and 1.
- Context/cache/retention controls: race, three repetitions each, seeds 7 and 42, CGO 1.
- Full root normal tests, vet and pinned staticcheck v0.8.2: CGO 0 and 1. Module verification passed.

The preceding full durable race at `08a2973` predates this test. No new full durable race or performance comparison was run for this test-only slice.

## Off-line protocol still open

`Session.ContextView` currently holds the Session line through `taskSnapshot`, cache access, history derivation and detachment. Publication cannot race that derivation. The source's paused off-line scan case has no native equivalent yet; serial exclusion does not establish its overlap or work-bound contract.

A future split must preserve these boundaries:

1. Capture admitted published bounds and any retained range under the Session line. Native reads must use the published image, while wrappers must retain their authoritative data and errors.
2. Derive from an owned immutable input outside the line. Current `contextRange` values contain mutable sequence, currentness and retention fields; handing their pointers to an off-line reader would race publication and retention.
3. Re-enter the line before cache installation. Reject stale installation when publication, conversation visibility/head changes, retention/settings changes, failure or Close has invalidated its witness. A late historical result must not replace a newer current cache.
4. A result captured before publication may still describe its captured bounds. It must not become a current range that skips entries published during the read. Test both publication during derivation and publication between derivation and cache installation.
5. Preserve native ownership/cancellation/health admission, detached results, wrapper projection and first-failure/Close behaviour. Keep Session commits serial and grouping disabled.

No off-line reader, new public API, cache installation witness or scheduler overlap was added in this checkpoint. Ordered expiry, task-root pins, inactive Wake and broader convergence retain their existing open dispositions.
