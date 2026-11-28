# Unchanged Durable cancellation barriers

This bounded packet compares the unchanged pi v1.7.0 `packages/durable/test/harness-cancellation-barrier.test.ts` at `fb0fe823539f6d04850f6e34748c01f01f212e1f` with public SQLite behavior from exact native main `393e8ed78fbcef88b22a58a7447a08d55dd5f402`. It owns cancellation publication barriers, fail-fast/restart precedence, explicit restart-mark upgrades and held outcomes. It does not claim broader provider, retention, migration, Chord or release acceptance.

## Pinned original

The original matched Git blob `2f59177c419521fa29d5c3b5227f494cc9ddd694` and SHA-256 `4ecfb6d2eb98e9b072c558d11b7b22f763ad11a48ee6ef201b53328d4ca66ed2` at the detached pin. Node `v26.13.0` and Vitest `4.1.12` passed all **4/4** tests, with zero skips. The command used the exact built AI workspace, the shared `/tmp/go-ai/v170-shared-verification.lock`, `NODE_OPTIONS=--max-old-space-size=650` and one worker.

```sh
NODE_OPTIONS=--max-old-space-size=650 GOMEMLIMIT=650MiB \
  flock /tmp/go-ai/v170-shared-verification.lock \
  ../../node_modules/.bin/vitest \
  --run test/harness-cancellation-barrier.test.ts --maxWorkers=1
```

An earlier source attempt in another retained stage found the absent generated AI `dist` exports and was not counted. The retained exact-pin run used the completed build and did not alter the original test.

## Public SQLite comparison and fixes

Three neutral native tests exercise the real public SQLite store and scheduler. The storage-decorator test blocks only the selected task-mark commit at `Storage.Commit`; the remaining fixtures use SQLite-backed open, close, reopen and public task reads. Existing focused controls cover the explicit top-abort upgrade assertion.

- `TestTaskCancellationBarriersPreventUncommittedCascadePhaseAdvance` passed on unchanged main: an invocation cannot start the next phase while its cascade mark is still unpublished.
- `TestFailFastCancellationSupersedesRestartOnlyDescendantMarks` timed out on unchanged main. The scheduler wrote a restart reason before considering a direct failFast selection. A same-pass selected cancellation now keeps the explicit empty reason, allowing a missing task definition to orphan the task.
- `TestTask120ExplicitAbortSupersedesRestartBlockedChild` passed as the source's later explicit-owner control: a top abort eventually supersedes a restart-only child mark and drains the tree.
- `TestRestartMarkOnHeldOutcomeDrainsOwnedWorkAndKeepsOutcome` timed out on unchanged main. Open-time recovery discarded a `completing` `AbandonOnRestart` task merely because its outcome was already decided. Recovery now marks it restart, cascades through owned live work and preserves the held completion.

The first fix changes only the reason assigned to a descendant already selected by fail-fast cancellation. The second admits a nonterminal held decision to the existing one-write restart-mark path. Neither fix changes ownership traversal, cancellation admission, storage writes, task input/result data, callback delivery or redaction. SQLite remains the only backend exercised.

## Focused evidence

The native selector includes the two repaired translations, the uncommitted-barrier control, nearest-owner reason, eventual explicit supersession and held-outcome ownership control. It passed twice with no skips after the fix under Go `1.27.3`, `CGO_ENABLED=0`, `GOFLAGS=-p=1`, `GOMAXPROCS=2`, `GOGC=60`, `GOMEMLIMIT=650MiB` and the same shared lock.

Additional ownership/cancellation and redaction controls are listed in the assertion ledger. `go vet ./durable` and pinned Staticcheck `v0.8.2` for `./durable` are the scoped static gates. This receipt is a bounded development comparison, not full acceptance, acceptance profiling or a push receipt.

## Limits

The original JavaScript objects are represented by detached Go `TaskRecord` values. The source `MemoryStorage` call is retained as an original execution receipt; native behavior uses public SQLite fixtures and a narrow Storage decorator only at the commit barrier. `Harness.inspect` state names and host object identity are not represented as JavaScript identity assertions. No provider, network, migration, Chord, release or full-suite claim is made.
