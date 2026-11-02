# Gated-generation view oracle

The pinned `harness-view.test.ts:128–158` scenario exposed two native differences: generation status used whole-document replacements, and attempt presentation was persisted immediately. The operation shape is fixed; deferred persistence remains a demonstrated failure.

## Preserved scenario

The native test starts a watch, counts stored touching commits independently, submits hi, waits for a delivered generation marker, releases the gated provider answer a longer answer, waits for submission settlement and actual Harness idle, then replays frames and compares the final committed view. It checks the initial user append and literal `["s",["docs","pi.live","generation"],{"attempt":1}]` tuple. No callback acknowledgements are inserted between production commits.

The fake provider emits a native terminal event instead of the source faux text stream. Default native follow-up input represents source type:input. Native kind:message/value.role:user represents source kind:pi.user; the test checks that envelope explicitly, together with append path, index zero, delete count zero and exactly one inserted record. The fixture uses native model/system/document setup. Shared identity and general JavaScript timing equivalence remain open.

## Independent stored counter

Source touches() synchronously observes stored commit publications. Native has no equivalent synchronous observer API. The test installs an append hook while both admission lines are held, delegates the original backend append and increments only after success. It decodes actual commit records and counts one touch per stored commit containing a root entry, a non-empty delta for a mounted conversation document, or a changed/created full mounted document. It never derives the count from watch callbacks or invents an acknowledgement to obtain the expected count. This classifier covers the fixture's actual writes; arbitrary net-zero/multi-write source observer equivalence remains separate.

The first counter probe omitted document deltas and reported two touches. After including the actual delta writes, all three backends consistently report four touches and four frames. Initial operation comparison also used the wrong native container shape and was corrected before confirming the literal tuple failure. The final recorder awaits equality with the complete independently captured idle view; an entry-ID-only wait could miss a later cleanup frame with the same last entry.

## Status publication fix

Native syncGenerationStatuses previously exposed attempt:0 from the prepared intent, then replaced all pi.live fields for attempt:1. Pinned generation.ts:269–279 instead presents the charged attempt by assigning live.generation as a whole field. Native status projection now leaves an uncharged intent without a generation marker and records explicit whole-field generation/tools sets or deletes. It uses private sealed-transaction staging and validates accumulated operations; public transaction authority is never reopened. Task attempt charging, request intent persistence, final values, cancellation, ownership and provider dispatch fences remain in place.

The active `TestMainViewUpstreamGenerationValueOperations` covers the value/operation subset. Restoring the old production file via overlay rejects the generation tuple on all three backends.

## Deferred persistence: unresolved

The complete assertion remains in the executable fixture `durable/testdata/upstream-main/pending/view_generation_persistence_test.go`. It has no skip or relaxed count. Its README documents the explicit overlay invocation, which returns non-zero. It is outside default Go test discovery; normal development success does not close this assertion.

Three repetitions/shuffle 42 after the operation fix fail all nine backend leaves with frames=4, stored touches=4; source requires frames=touches+1. Source generation.ts assigns attempt presentation using PROGRESS_COMMIT. live.ts:230–235 specifies deferred presentation storage at the next commit reading/writing pi.live or at the configured deadline. Session.commitWith validates deferred persistence and passes it into the commit pipeline independently of scheduler intent durability.

Native generation.go:174–194 charges/persists the task attempt before remote dispatch, and syncGenerationStatuses projects live state into that same durable commit. Native Session lacks deferred publication/storage settlement. Removing the intent write, filtering the storage counter or manufacturing an extra callback would break the relevant contracts or mask the failure.

Next implementation work is a bounded deferred-presentation path with separate publication/durability witnesses: preserve immediate task intent charging, publish attempt presentation independently, flush it on dependent live reads/writes or its deadline, and preserve Close/failure/crash recovery and receipt sequencing. Current published-image adoption requires contiguous durable receipt sequences, so volatile publication cannot simply reuse a successful durable receipt. This dependency is unresolved; no retained divergence has been accepted.

## Official 1.5.0 memory prerequisites

[Conversation-view memory controls](../upstream-v1.5.0/conversation-memory.md) pass unchanged native runtime `148c77b`: early view delivery, acquisition during a hold, own-only dependent flushes and stored-timer/backlog replay. These standalone Session controls do not enable Harness generation deferral. The full executable generation oracle above was not rerun; its recorded baseline and remaining intent/presentation split are unchanged.

## Development checks

Go 1.27.3, GOMAXPROCS=4, project environment sourced:

- New value/operation test: normal ten repetitions/shuffle 42, CGO 1/0 pass in 1.020s/0.581s.
- New test plus selected generation-abort/AgentPatch tests: race three repetitions/shuffle 7 passes in 7.017s.
- All TestGeneration-prefixed tests plus the actual committed-live partial/tool-slot test and new oracle: normal three repetitions/shuffle 42 CGO 1/0 pass in 7.489s/7.345s; race once/shuffle 42 passes in 16.373s.
- Durable vet/staticcheck v0.8.2 pass both CGO modes; module verification and diff checks pass.
- Full pending oracle deliberately fails nine backend leaves; old-production tuple mutation fails three leaves. Disposable overlays, source copies and logs are removed after analysis.

The continuation prototype remains untracked/excluded and content-compared after temporary staticcheck relocation. No full-suite/full-race, performance or acceptance profiling was performed. No dependency, persistence-format, asset or release-ref change. Full source line128 convergence remains incomplete.
