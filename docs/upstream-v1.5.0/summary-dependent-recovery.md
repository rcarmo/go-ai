# Summary recovery and third-party dependent reads

These tests pass unchanged production at native baseline `21a4c23976a98919422ec34869b27f890a6604e8`; runtime remains `decbd9d4419d0580c6b84e6832b07d7893573444`. Upstream is official v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`. This checkpoint adds tests and documentation only.

## Summarisation interrupted by process loss

`durable/harness_summary_recovery_main_test.go::TestMainHarnessSummarisationCrashResumesCheckpoint` translates `packages/durable/test/harness-deferred-progress.test.ts:398-426`.

It uses the source's three `u1/u2/u3` and `a1/a2/a3` messages, each followed by100 repetitions of `word `, with automatic compaction disabled, reserve1000, keep-recent150 and background0. It starts manual compaction, waits for the summary provider to enter, fails the Session, joins the host and reopens actual storage. The original task ID must complete, a `pi.compaction` entry must appear, and the existing live document must be empty.

Native controls additionally require durable `summarize` intent before dispatch, the same pinned summary input on restart, exactly five provider calls, exactly one compaction entry and a `SUMMARY` model contribution. Native entries carry that contribution in `Entry.Model`, not `Entry.Value`. Existing compaction tests covered reopen after completion and after retry sleep, not loss during an active provider call; those completed tests were inspected, not rerun.

## A real third-party Runtime.Commit

`durable/harness_dependent_read_main_test.go::TestMainHarnessThirdPartyCommitStoresDerivedPartialAtomically` translates the source scenario at430-475. It is distinct from the earlier Harness-commit dependent-read controls.

A registered native task uses public `Runtime.Commit`, `Tx.Document`, `DocumentHandle.Get` and `Tx.AppendEntry`. Before task creation, an actual watch acknowledges a held prefix longer than40 characters, and storage has no partial. The task reads that logical value and appends its derived text. Interception of the real storage append checks that the audit entry and a delta for the live document occur in the **same** commit record. This is not a synthetic publication or an inferred final-state check. After failed-Session shutdown, storage must contain the derived partial. Reopening the original submission must yield that exact aborted answer followed by `answer`.

Native adapters are explicit: a known document ID addresses the builtin instead of a TypeScript document-definition object; `Entry.Value` stores `{text: string}` instead of scalar `Entry.data`; the native `delta-document` storage record corresponds to source `document.change`. The held text is a controlled prefix of source `LONG = "streamed ".repeat(400)`. The public task path, immediate commit, atomic dependency storage and recovery assertions are preserved.

## Validation

Only the two new functions ran. There was no runtime edit and no repetition of completed generation/tool/Session suites.

Go1.27.3, `GOMAXPROCS=4`, `scripts/project-env.sh`, protected-prototype overlay:

| Check | Result |
| --- | --- |
| Corrected unchanged-production baseline, CGO1, six backend leaves | Pass,1.228s |
| Normal count5/shuffle42, CGO1 / CGO0 | Pass,4.905s /2.466s |
| Race count3/shuffle7, CGO1 | Pass,14.087s |
| Durable vet / Staticcheck v0.8.2, both CGO modes | Pass |
| Module verification / formatting / diff / protected prototype | Pass |

The test pattern is `^TestMainHarness(SummarisationCrashResumesCheckpoint|ThirdPartyCommitStoresDerivedPartialAtomically)$`. Staticcheck uses an exact physical source copy including both new files and excluding the untracked prototype.

Three active faults fail all three backend leaves each: skipping summary execution on restart, changing its pinned request on restart, and omitting the native audit task's held-document dependency. The latter fails the same-transaction document assertion and crash-recovery checks. Initial fixture build errors, a missing nil guard for the in-memory append callback, and an incorrect lookup of summary content in `Entry.Value` are excluded from validation; they were not production failures.

The canonical release review and generated362-row crosswalk change one evidence cell only. Dispositions, change markers, inventory, the210-row dependency queue and historical reviews remain unchanged. Owned source copies, overlays, physical packages and logs are disposed after use; installed dependencies/toolchains and the continuing pinned source cache are retained.

## Remaining work

Source398 and source430 now have native scenario translations. The subsequent [eleven-declaration assertion map](harness-deferred-assertions.md) adds four explicit checks and now records matching literal retry overrides for the nine source slow fixtures. Complete TypeScript body equivalence, controlled clocks/provider pacing and document/entry/callback adaptations are not accepted by these nominal outcomes. Shared public identity, tracker/WeakRef, synchronous observer/reporting contracts and broader abort ordering remain open. Source228 remains deferred polling, not abort-during-flush.

Cross-package/schema/support/catalogue correspondence, broader authorised acceptance and CPU/allocation profiling remain unverified. No full-suite, profiling, dependency/storage-format, CI, release or full-v1.5.0-alignment result is claimed.
