# Upstream conformance completion report

**Overall migration: incomplete. Full parity and release acceptance are not established.** This report closes a verified development slice, not the complete upstream port.

Target: official v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`. Native implementation: `/work/go-ai/durable`, authorised SQLite-only, pure Go. The preceding pushed checkpoint is `e347fb9` (caller-owned admission).

## Completed in this continuation

- Ported the full atomic busy-steer assertion packet: the host checks Live, writes its own document, admits input, then appends its own entry in one stored commit. Sent IDs, the entire Inbox item and the active run's original inputs match the pinned assertions.
- Replaced private queued input envelopes with canonical `content` and optional `entry` metadata. Block input and explicit-null data retain their original shape.
- Queued passive writes now contain a canonical EntryDraft. The same encoder supplies compaction-write queue production; placement IDs, task attribution and the private Value field are excluded.
- Verified idle input requests progress itself, without Resume or Submission.Wait masking an omitted admission wake-up.
- Verified failed-run continuation inside a caller transaction, its write fence and subsequent host entry.
- Added a definite SQLite failure/reopen control: failure identity survives and no submission, task, entry or run survives reopening. This is not complete arbitrary/uncertain Storage-failure acceptance.
- Verified canonical queued model-visible writes and eventual placement/completion. This fixture holds a provider, not the original tool boundary; that source variant remains open.

The exact input/write shape tests failed before the fixes. Idle progress and continuation already passed and required no speculative runtime changes. A failure/reopen fixture initially tried to close storage already owned/closed by Session; correcting the fixture required no runtime fix. Tests remain in the existing version-neutral transaction suite.

## Verification

| Check | Result |
|---|---|
| Full `./durable/...`, uncached, sequential | durable **418.205s**; tools **19.360s** |
| Ten focused shuffled repetitions, seed42 | **16.943s** |
| Repository vet, Staticcheck v0.8.2, logging | Pass |
| Deliberate compiler faults | Three semantic failures: noncanonical input envelope, missing canonical write entry, omitted admission progress request |
| Original-source oracle | **189 unchanged cases +49 independent probes**, pass |
| Register | 29 groups; 165 review obligations across 101 source test files; source/tree and link checks |

The reference total comprises 181 tests across nine unchanged upstream files, eight unchanged Storage conformance cases,35 admission probes and 14 other probes. Those tests run the original implementation; they are not 189 accepted native cases. All 23 original submit-in-commit tests execute, but native counterparts for every assertion are not complete.

The retained source snapshot has no `.git`: retained hashes and the complete test tree are checked, but commit identity is not independently reverified. Verification uses `CGO_ENABLED=0`, `GOMAXPROCS=2`, `-p 1`; no race-detector coverage is claimed. This is ordinary development verification, not pre-release CPU/allocation acceptance. Disposable fault overlays are deleted.

## Remaining completion blockers

| Area | Remaining work |
|---|---|
| Canonical builtin execution — D07/D09 | Normal execution still uses historical private generation/tool checkpoints and submission payloads. Canonical queued document production does not establish imported-queue execution, full migrations or recovery interoperability. |
| Storage interfaces and persistence — D10–D12 | Complete wrapper/public-interface authority, original SQLite schema/record interoperability, and remaining result/query DTO assertions. |
| Commit/public API — D13 | Original callback-result/options semantics and full exported-symbol mapping. Go Commit still returns a sequence and uses caller-side result capture. |
| Publication/admission — D01–D06 | Exhaustive dependencies, bounded outcomes, caller-continuation turns, deferred persistence and close/error ordering. D04 is not accepted. |
| Submission/lifecycle — D20/D21 | Original tool-bound write placement, arbitrary/uncertain Storage failures, trace/link/span propagation, task-bound lifetime/close and exhaustive resume/scheduling assertions. The earlier queued progress/close error-order caveat remains open. |
| Readonly/document/view lifetimes — D14–D17 | Complete mount/collection/cache/reload/lifecycle and observer-dispatch acceptance, beyond the mapped passing identity assertions. |
| Resources, ownership and external viewers — D18/D19/D27 | Complete resource/numeric/ownership/race corpus and read-only concurrent SQLite viewers. |
| Codemode/schema/provider — D22–D24 | Complete worker/restart, dependency/compiler/RegExp and inherited AI/provider acceptance. |
| Final verification — D26 | Full assertion-level crosswalk, complete regression/fault evidence and pre-release CPU, alloc_space and alloc_objects analysis/tuning. |

D08, D28 and D29 are recorded as fixed; other groups contain partial progress, confirmed deviations or unverified obligations. Alternative storage engines remain outside the user's authorised SQLite-only scope (D25). Neither those status labels nor green development suites define a completion percentage.

The next acceptance work is the remaining submission tool/trace/lifetime packet and canonical builtin execution/migrations. The complete register, including causes, evidence and acceptance criteria, remains authoritative: [deviations](../docs/upstream-v1.5.0/durable-deviations.md), [structured gaps](../docs/upstream-v1.5.0/durable-parity-gaps.json), [assertion crosswalk](../docs/upstream-v1.5.0/task-table-assertions.tsv). Detailed slice receipts are in [submission admission](submission-admission.md).
