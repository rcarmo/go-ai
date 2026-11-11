# Builtin task publication and progression

Normal model-tool runs now retain their public stored observer through completion. The source-derived acceptance packet that failed at `1eafca1` is in the ordinary suite as `TestMainBuiltinCanonicalPublicationsV150`; its disposable overlay runner is removed.

Target: pi-durable v1.5.0 at `f9a26365568127461c47a07a37dcb42c140a3ac5`. This is a bounded projection slice. The runtime still stores private generation/tool checkpoints; canonical raw storage, nested tools and full migration acceptance are open.

## Persisted facts and public records

- Generation records expose version2, empty input and source-shaped preparation/request/tool-wait checkpoints. The request intent now charges its attempt and stores the newest included entry as `cutoff` in the same transaction. Charging at later dispatch previously published an intermediate attempt0/cutoff0 request.
- Model-tool checkpoints persist their actual tool-calling assistant entry before sequential calls are queued. Public version3 records use `{kind:"model",assistant,callId}`, then `call` and `execute` states. Completed, failed and aborted model results now include `{kind:"model",entryId}`; successful results retain control. An abort no longer invents a reason string. The exact failed/aborted/control public unions still need independent source/native acceptance beyond existing runtime regressions.
- Scoped source arguments preserve lone UTF-16 units and WTF-8 keys through task lookup, bounded public encoding, readonly stored publication, explicit mutable copies and ownership copies. Untrusted JSON receives no new string/key/marshaler authority. The generic publication JSON roundtrip was replaced for task records only; otherwise it would erase these units.
- Clean close/reopen resends the same charged request. The pinned SQLite probe records two provider calls at attempt1, unchanged cutoff/options and no restart charge. The obsolete native assertion expecting attempt2 is now1. Actual crashed-running recovery still uses the separate interrupted-attempt path. Its existing tests remain in the full regression suite.
- Preparation-rejection synchronisation now matches charged intent1 instead of the former intent0. All rejection, wake-sequence and cancellation assertions are retained.

## Test-first evidence

At `1eafca1`, the real Harness acceptance packet failed generation version/state, ended early with `listener_error: task record input is absent`, lost assistant/tool publications and had the wrong final ending. Its source equivalent passed. The added sequential/parallel progression packet failed against the old runtime too. Restoring only the old generation implementation exposed attempt0/cutoff0; removing the model result discriminant failed exact terminal-state assertions.

The first broad native run after moving attempt ownership failed clean-close, UTF-16 snapshot and preparation-fallback tests. Source probing resolved clean-close; the fallback fixture's observation predicate was obsolete. An initial UTF-16 fallback kept the old snapshot green but exposed a private checkpoint. The strengthened public task assertion failed that fallback before the exact scoped representation replaced it. The final UTF-16 test also observes the real public stored lane, checks immutable/mutable ownership and rejects untyped UTF-16 JSON.

`TestBuiltinTaskPublicProgressionMatchesSourceV150` compares exact input/version/state unions for two tools in parallel and sequential modes, binding IDs to real records. It collapses adjacent identical states; publication count, cross-record ordering and commit granularity have separate open acceptance. The retained independent source progression probe uses the same scenarios and assertions. These are source-derived controls, not translated named upstream tests.

## Verification

Pure Go, SQLite, sequential jobs:

- Full durable **419.068s**, tools **19.173s**.
- Publication/progression, UTF-16, clean-close, preparation-fallback and tool-control selections: count10/shuffle42, **59.930s**.
- Readonly UTF-16 subtree/progression selection: count10/shuffle42, **21.368s**. Nested map, array and isolated scalar handles retain their scope when independently owned/encoded; untrusted equivalents are rejected.
- Repository-wide vet, Staticcheck v0.8.2 and logging gate pass.
- Reference: **145 unchanged pinned tests +34 independent probes**, including retained sequential/parallel progression and real SQLite orderly-close/resend. Source hashes and full test/support trees are checked; the retained snapshot has no `.git`, so commit identity is not independently reverified.

These development checks include no acceptance profiling or race-detector coverage. Pre-release CPU, alloc_space, alloc_objects and measured tuning remain required.

## Remaining boundary

D09/D16 remain open. Canonical `pi.generation`/`pi.tool` raw Storage writes are still refused; public projections do not make stored envelopes reversible. Old model-tool checkpoints without assistant witnesses require migration rather than invented IDs. Nested tasks still have a separate native kind/input/version/result. Unoffered calls skip task creation natively. Unknown generation phases, complete retry/poll/overflow/compaction/finish/drain states, optional fields, settings and version migrations need source-derived assertion packets. Public UTF-16 task encoding is covered here; public raw decoding/storage and transcript receipt carriers still need end-to-end canonical acceptance. D04's caller-continuation bridge remains absent.
