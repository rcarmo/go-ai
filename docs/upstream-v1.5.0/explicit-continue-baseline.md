# Explicit continue: focused implementation and baseline

Runtime `b66619a5169405a993ea9187d44185c171385d05` adds idle, entryless `Input.Type = "continue"` admission and settlement. The focused pure-Go gate passes **29 functions, three repetitions, 87 top-level passes, zero failures/skips** in 18.493s. A real SQLite crash/reopen test also passes. Full v1.5.0 alignment and complete source-body parity are unverified.

## Pins and scope

The target is official upstream v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`. The exact [`packages/durable/test/harness-continue.test.ts`][tests] was read in full: **423 lines, 13 declarations**, supplied as `/workspace/tmp/go-ai/runs/explicit-continue/20261102/source-continue.test.ts`. The source map below gives **no full-body translation credit to any declaration**. Existing automatic continuation and boundary controls retain their separate scope in [boundary-first.md](boundary-first.md).

The original public admission behaviour comes from native `375386c`. The executed initial baseline used the local pure-Go SQLite checkpoint `cef0196` plus the preserved four-file continuation WIP (`generation_handoff.go`, `storage.go`, `submission_status.go`, `submission_transaction.go`); it was not a clean `375386c` checkout. Admission still rejected the new type. The source test and generation mirrors were checked against their exact Git blobs at the release pin: `627e6218fd5400d5213808b15f500ba240b6668b` and `17c28db4749f6dc7d1f56f7fe9a8c0cc8c2d14dc`. An earlier provenance probe used the cache parent instead of `cache/upstream-pi/pi-repo`; that error was corrected before verification.

## Admission and run contract

| Contract | Exact release behaviour |
| --- | --- |
| Request-ID deduplication before busy rejection | [`admitSubmission`][admit] first looks up a supplied request ID in the conversation. A matching type returns the existing submission without writing, even while busy. A different type for that ID errors. A fresh continue while busy throws `ConversationBusy` before any write. |
| Entryless admission and status | An idle continue is never queued. Admission creates a `type: "continue", status: "placed"` submission, applies the final boundary and starts its run in one commit. The continue itself appends no entry and adds no user message. Its later settlement can be `done` with an answer or `unanswered` with a reason. |
| Queued users | The final boundary places selected users left by the failed run before starting the new run. Its inputs are exactly `[...users, continueId]`; those users and the continue share the resulting answer. Passive writes/reset entries retain their boundary roles. |
| Inherited `first` | Explicit admission passes `live.runFailure?.first ?? first` to [`startRun`][start]. The failed run's boundary wins when present; otherwise the newly applied boundary supplies `first`, which may be absent. `startRun` clears the old failure and starts at zero turns. This inherited-history assertion is source-derived; none of the 13 bodies directly checks `first`. |
| Provider context | [`prepare`][prepare] ignores trailing system messages when finding the last message the model can answer. A last non-system `user` or `toolResult` permits a request. Empty context or an existing text answer ends `unanswered/nothing_to_answer` without a provider request. A cut-off assistant tool call can leave missing tool results to answer; a cut-off text-only answer cannot. |
| Failure and trace | [`failNothingToAnswer`][nothing] uses the failed-run path and records `nothing_to_answer`. With `afterFailedRun: "continue"`, queued inputs can start afterwards. Continue admission starts a root submit trace linked to its caller, as input admission does; the submission stores that trace. |

Explicit continue creates a new run without a new user entry. Automatic failed-run continuation, `onYield` continuation and native `AfterResponse` hooks have distinct contracts. Their passing controls supply no explicit-continue full-body credit.

## Thirteen source declarations

Line numbers identify the declaration in the pinned source. Every row has **none** for full-body credit; the table records required assertions, not observed native outcomes.

| Line | Source declaration and assertions to retain | Full-body credit |
| --- | --- | --- |
| [98][s98] | **answers a run that failed, without a new user message** — first input stays `unanswered/model_error`; failure is visible. Continue reports `placed`, settles as type `continue`, answers `recovered`, sends context ending in `user`, adds no user entry (count stays one) and clears the failure. | None |
| [121][s121] | **answers the tool results of a round aborted midway** — wait for the real tool to start, abort the run, require the original input `unanswered/aborted`, then answer `after the abort` with provider context ending in `toolResult`. | None |
| [153][s153] | **skips system messages when deciding there is something to answer** — change instructions between inputs; after the second input fails, context really ends in `system`. Continue still answers `resumed`. | None |
| [172][s172] | **is rejected while busy, after request ID deduplication** — gate the active continue; a fresh continue throws `ConversationBusy`, while request ID `resume-1` returns the existing submission ID. | None |
| [187][s187] | **ends without a request when there is nothing to answer** — empty and already-answered contexts settle `unanswered/nothing_to_answer`; the empty settlement has type `continue`. The latter adds no provider call and projects `runFailure.reason = "nothing_to_answer"`. | None |
| [204][s204] | **continues an answer with tool calls cut off at its output limit, which ends in their missing results** — the original length-limited run is `done`; continue answers `resumed` with final provider roles `["assistant", "toolResult"]`. | None |
| [221][s221] | **does not continue a text answer cut off at its output limit** — continue settles `unanswered/nothing_to_answer`; total provider calls stay one. | None |
| [233][s233] | **answers a run interrupted by a host that died while the model answered** — persist streaming progress, kill and reopen SQLite with retries disabled; the old input settles `unanswered/interrupted`. Continue answers `after the crash`; its request ends in `user`, excluding the aborted partial. | None |
| [262][s262] | **places a reset a failed run kept, and finds nothing to answer in the context it starts** — queue reset during the gated failing request. Require one retained inbox item, then continue `unanswered/nothing_to_answer`, empty inbox, no non-system context and only the original provider call. | None |
| [290][s290] | **starts the inputs queued behind a continue with nothing to answer under afterFailedRun continue** — after an answered input, admit continue then follow-up input; verify the latter is queued. Continue fails `nothing_to_answer`, then the queued input answers `second answered`. | None |
| [306][s306] | **is submitted by a tool through the handle of a conversation it owns, and rejected there while busy** — create/configure the actual task-owned child; its input fails, its continue succeeds, and a second continue through that handle throws `ConversationBusy` while the first is gated. Parent/tool execution completes. | None |
| [356][s356] | **starts a root trace linked to its caller, as an input does** — submit span has `parentId: null`, result `placed` and exactly the caller link; the submission stores its context. A prepare-phase span has that submit context as parent; all spans settle. | None |
| [395][s395] | **places inputs a failed run left queued first, and they join its run** — the failed run leaves the second input queued. Continue starts with `live.run.inputs = [queued.id, resumed.id]`; both settle with the same answer, `both handled`. | None |

The crash body at line 233 now runs against a real modernc SQLite file under CGO=0. The native adapter fails the actual Session, waits for its failed ending, joins the host and reopens the same file. It checks a stored 64-character partial, disabled recovery retries, the old `unanswered/interrupted` input, a successful entryless continuation and the actual provider context excluding the aborted partial. This simulates process loss; it does not establish OS-kill or power-loss durability. Entrylessness beyond user-count checks, atomic admission, deduplication without writes and inherited `first` need separately labelled source-derived controls where the bodies do not assert them directly.

## Native assertions and adaptations

All functions below have the `TestMainContinue` prefix. The DTO/transaction controls use `TestMainSubmissionContinue`. Memory/journal helpers exercise native storage; the interrupted-host case uses real SQLite. Registered Go providers replace Faux. Native assistant entries persist their receipt in `Entry.Value`; `Entry.Model` is an application contribution field. Two initial persisted-answer fixtures mistakenly inspected `Model`; their receipt decoding was corrected without changing production storage or expected role/text. The handoff control retains the failed run's inherited `first`, even after a tool receipt.

| Source declaration | Native function suffix / scope |
| --- | --- |
| 98 | `FailedRunWithoutNewUserEntry` — original failure, placed/done continue, recovered answer, real user context, one user entry, cleared failure. |
| 121 | `AfterAbortedToolRound` — actual tool cancellation, original aborted input, recovery answer and final tool-result context. |
| 153 | `SkipsTrailingSystem` — changed instructions, second request failure, actual trailing-system precondition and recovery. |
| 172 | `BusyRejectAndRequestIDDedup` — gated run, exact busy error, same-ID deduplication and no writes on either path. |
| 187 | `NothingToAnswerEmptyAndAnswered` — entryless unanswered results, no extra request and exact failure reason. |
| 204 | `LengthLimitedToolCall` — literal calling/echo/c1 output-limit answer, missing-result context, recovered answer; extra control forbids any tool task. |
| 221 | `DoesNotResumeLengthLimitedText` — no continuation request after a text-only length answer. |
| 233 | `InterruptedSQLiteAfterStoredPartial` — actual file/reopen, stored progress and disabled-retry recovery described above. |
| 262 | `PlacesKeptReset` — real queued reset, retained inbox precondition, empty inbox/context afterwards and one request. |
| 290 | `NothingStartsQueuedInput` — host model-resolution gate replaces JS same-stack scheduling; follow-up is demonstrably queued behind the continue, then answers after its failure. |
| 306 | `OwnedToolAfterModelError` — actual tool-owned child, bound continue, busy rejection and parent completion; native abort/lifetime controls are additional. |
| 356 | `RootTraceAndAdmission` — root/caller-link, correct continue type/placed result, native queued-phase ancestry and settled callbacks. **The persisted `status.trace` assertion is unported**: `SubmissionRecord` has no serializable trace-context field. Callback/object identity and a host join adapt native telemetry. |
| 395 | `BoundaryKeptInputsAndWrite` — selected queued input precedes continue, both share the answer; passive-write/inherited-first/legacy-first controls extend this body. |

`ToolHandoffOrderedInputs`, `BoundaryLegacyFirstFallsBackToActualWrite`, owned-abort and six DTO/transaction functions add source-derived/native controls for ordering, failure, ownership, copies and storage unions. Whole-file review stays `pending-review`; no TypeScript/Vitest execution or full-body credit is recorded.

## Observed checks

Go 1.27.3, GOMAXPROCS=4, CGO_ENABLED=0, with `scripts/project-env.sh` sourced. Both logical and canonical overlay paths exclude only the protected prototype. Ordinary runs disable child profiling. The final repeat routes TMPDIR/TMP/TEMP to the owned run’s tests directory and GOTMPDIR to its build directory; it repeats the same 87 passing checks after isolating those paths.

| Check | Result |
| --- | --- |
| Initial preserved-WIP baseline | 13 top-level functions: 6 pass, 7 fail, no skips, 1.299s. Public continue admissions fail `durable: unsupported operation`. |
| New empty/answered/length-text/no-model controls before their fixes | Three functions fail, 6.375s. Empty/text continuations dispatch extra requests; no-model attempts to place an illegal continue user entry. |
| Literal length-tool baseline | One function fails on both backends, 0.090s: `unexpected_tool_call` incorrectly changes an output-limit answer into failure. |
| Selected trace baseline | One function fails on both backends, 0.137s: submit attributes report input/queued instead of continue/placed. Root/link/phase ancestry already pass. |
| First dedicated SQLite run | Pass, 0.149s; included again in the final repeated gate. |
| Final focused gate | 23 new functions + six affected transaction/no-model/atomic-boundary/telemetry regressions; count=3, shuffle=42: 87 pass, zero fail/skip, 18.493s. |
| Static validation | `go vet` durable with prototype overlay; Staticcheck v0.8.2 with all tests in an exact physical copy excluding the prototype; formatting and diff checks pass. |
| Active faults | Busy bypass, lost inherited first, reversed inputs, missing nothing-to-answer guard, old length-tool rejection, wrong trace type and wrong placed result each fail two backend leaves. |

Reproduce the focused gate from the repository root (the prototype remains in place):

```sh
. scripts/project-env.sh
export GOTOOLCHAIN=go1.27.3 GOMAXPROCS=4 CGO_ENABLED=0 GO_AI_PROFILE_CHILDREN=0
run="$PROJECT_TMP_ROOT/runs/explicit-continue/recheck"
mkdir -p "$run/tests" "$run/build"
export TMPDIR="$run/tests" TMP="$run/tests" TEMP="$run/tests" GOTMPDIR="$run/build"
printf 'package durable\n' > "$run/excluded.go"
export CONTINUE_RECHECK="$run"
bun -e 'import{realpathSync}from"node:fs";const p=process.env.PWD,r=process.env.CONTINUE_RECHECK;await Bun.write(r+"/overlay.json",JSON.stringify({Replace:{[p+"/durable/continuation_prototype_main_test.go"]:r+"/excluded.go",[realpathSync(p)+"/durable/continuation_prototype_main_test.go"]:r+"/excluded.go"}}));'
pattern='^(TestMainContinue|TestMainSubmissionContinue|TestSubmissionTransaction|TestMainBoundaryAtomicSuccessorPublication$|TestUnconfiguredConversationNoModelFailsWithoutSyntheticAssistantAndCanConfigure$|TestTelemetry140(SubmitInputUsesCommittedAdmissionResult|InputRootsLinkCallersAndWritesRemainChildren|NestedTasksInheritCreatingPhaseAndReleaseParents)$)'
go test -overlay "$run/overlay.json" ./durable -run "$pattern" -count=3 -shuffle=42 -timeout=180s
go vet -overlay "$run/overlay.json" ./durable
```

Remove that owned recheck directory after examining its result. Existing SQLite conformance/security gates retain their earlier checkpoint and were not repeated for this continuation slice.

The first fault attempt omitted the logical source overlay path and passed without exercising the mutant; it receives no fault credit. Both paths were then supplied, an invalid-syntax execution probe confirmed activation, and all seven behavioural faults were rejected. A draft follow-up fixture used a nonexistent `Input.FollowUp` field; correcting it to `WhenBusy: "follow-up"` preceded behavioural baseline credit.

Runtime changes preserve entrylessness in normal and no-model preparation, share terminal settlement with selected inputs, retain live input order across handoff and keep failed assistant receipts separate from continue answers. Nothing-to-answer uses the existing pinned failed-run policy and durable terminal hold machinery. Output-limit tool calls stay in the answered transcript and never enter `acceptTools`; existing call/argument validation remains. No dependencies, checkpoint fields, public Commit semantics or published assets changed.

Disposable source mirrors, overlays, copied packages, fault variants and logs are removed after analysis. Only these conclusions and source tests are retained; the pinned Git mirror, installed dependencies/toolchains, native v1.4.0 and the protected untracked prototype are preserved.

## Adjacent work and execution limits

The `nothing_to_answer` cases above are part of the continue corpus. Additional source-derived nothing-to-answer controls need their own results. The complete [`harness-turn-limit.test.ts`][turn-limit] contract and [`33-turn-limit-and-continue.ts`][turn-example] example are separate work: these 13 declarations do not establish full turn-limit behaviour or whole-file parity for that suite.

Validation is **pure Go, `CGO_ENABLED=0`**, with memory/journal adapters and the dedicated SQLite crash test. No CGO=1 or race exception, full suite, profiling, hosted CI or release publication was run. Broader storage/platform parity, upstream `afterRun`, full mixed boundaries, remaining restart-limit/report/settings contracts, persisted trace context and complete callback/record/clock equivalence require further work. Code mode continues to use the existing QuickJS guest in wazero; its wider v1.5.0 audit is separate.

[tests]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts
[admit]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/submissions.ts#L228-L273
[start]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L1157-L1173
[prepare]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L202-L208
[nothing]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/src/harness/generation.ts#L615-L654
[s98]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L98
[s121]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L121
[s153]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L153
[s172]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L172
[s187]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L187
[s204]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L204
[s221]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L221
[s233]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L233
[s262]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L262
[s290]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L290
[s306]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L306
[s356]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L356
[s395]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-continue.test.ts#L395
[turn-limit]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/harness-turn-limit.test.ts
[turn-example]: https://github.com/earendil-works/pi/blob/f9a26365568127461c47a07a37dcb42c140a3ac5/packages/durable/test/examples/33-turn-limit-and-continue.ts
