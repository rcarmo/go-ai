# Synchronous observers remain unimplemented

The native watch API does not implement pinned upstream document observers. The earlier review at `81f3cd412758ebdd92324c7d63b669a189408d0a` still applies; the stored-observer counterexample below is pinned to `c9f4c7b9fcd0f3e6b5bbc35c7c9b6cda35580eb8`.

## Reviewed contracts

- `packages/durable/src/session/session.ts`, `observeDoc` and `#observeDocument`: attach at the admitted publication boundary, return the current value, follow the document address and deliver each change synchronously during publication, outside storage adoption. Retirement preserves the preceding value; recreation replaces it. Owner termination detaches the commit subscription and ends the observer.
- The same file, `guardObserver`: contain synchronous throws and thenable rejections from `advance`. Report the original error, detach, and call `end` with `listener_error` if still attached. Self-detach suppresses that end. Contain and report errors from `end` too; these guarded errors do not fail commits or the Session.
- `session-observers.test.ts`: queued/cancelled attachment, detach during publication, another observer detaching a later observer, retirement/recreation, listener-error containment, async rejection and lifetime tests remain unmapped to a native observer API.
- `harness-observers.test.ts`: normal close fails a synchronous observer with `session closed`, while a watch ends normally; actual Session failure carries its first cause. These are different callback/end contracts.

Fatal failures of lower-level synchronous Session listeners must remain separate from guarded observer errors. The initial review conflated those layers; reading `guardObserver` corrected that interpretation. No observer-close error should be promoted to a fatal Session failure merely to fit the existing native failure path.

## Native publication boundary

`prepareSessionPublication`, `settlePreparedPublication` and `publishSessionPublication` execute under the Session line. They prepare detached watch work, settle storage, adopt the published image and enqueue observers. `Session.commit` releases the line before making callbacks runnable. Existing document watches serialize callbacks off-line and fold pending changes; commit subscriptions are also asynchronous. Neither supplies synchronous, per-publication observer delivery.

Existing asynchronous Go callbacks may reenter Session APIs, so moving them under the line would deadlock valid callers. Upstream's synchronous observer contract separately forbids blocking or calling the Session (`src/types.ts:1097-1124`; `session.ts:893-919`). A new observer lane can use that explicit contract while preserving legacy callbacks. Simply invoking callbacks after `leaveThen` releases the line would let a queued later commit publish before the earlier observer runs. Reusing watches would additionally fold away individual publications and use different error/ending semantics. These alternatives have not established the target contract and are not shipped as an observer API.

The implementation needs a publication notification phase with explicit re-entry/admission semantics, stable per-publication delivery order, detach-safe listener iteration and guarded lifetime/error handling. It must preserve existing blocking APIs, admission, cancellation, first-failure fencing and Close behaviour. Review that boundary alongside [publication ordering](durable-group-contract.md); overlap remains disabled. A blocking-callback restriction or changed legacy ordering is not implicitly accepted.

This is a partial source review, not a complete disposition of `session-observers.test.ts`, a production implementation, or an acceptance result. No prototype was added.

## Stored-observer counterexample at c9f4c7b9

`durable/testdata/upstream-main/pending/session_deferred_observers_test.go` translates the complete source521 progression and replica/count/change-type assertions through the real native writer and legacy stored subscription. The fixture is explicitly invoked outside default discovery. Source stored observers are synchronous; the native API adapter waits for its first three asynchronous callback records and leaves the Close assertion intact.

On unchanged `bd2123f`, Go 1.27.3/GOMAXPROCS4 yields three publications for four actual stored batches on memory/journal/SQLite (CGO1, 0.079s) and memory/journal (CGO0, 0.053s). The first three publication replicas match their real stored prefixes, and Storage reconstructs to `abcdef`. The fourth `["document"]` publication is absent. Both corrected runs fail only the publication count/type-array assertions. Initial result-type and immediate-mode adapter errors were corrected before these credited baselines; they are harness failures.

`Session.Close` currently calls `closeWatches()` before taking the line; that stops subscriptions and discards their pending calls. `flushHeldOnClose()` then settles/adopts storage without preparing any observer publication. Delaying `stop` alone cannot deliver the missing publication. Running legacy callbacks inline would violate their re-entry contract, and awaiting them during Close could deadlock a callback that calls Close. A dedicated synchronous stored-observer lane needs post-adoption delivery followed by its final ending, while legacy subscriptions and watches retain their existing behaviour.

The relevant source contracts were read at c9 before any production edit: `SessionImpl.close()` lines778-825, `observeCommits()` lines893-909, `followCommits()` lines911-920, stored publication lines1330-1405, and `guardObserver()` lines1770-1834. `session-observers.test.ts` bodies389/403/414/427 require ordered stored callbacks then normal-close failure, original failure cause, idempotent detach without a later end, and guarded listener errors that leave commits/other observers healthy. Normal close gives observers a per-close `Error("session closed")`; watches end normally. The guard reports before detach/end, suppresses an end after self-detach, releases source/observer references and contains ending errors; rejected thenables are guarded too. Source564 in the deferred file additionally requires Session-local memory/stored kinds and own-operation delivery, with no duplicate at timer flush.

Next implementation must port those controls first, preserve raw failure identity and publication-time listener membership/order, and keep reporting outside any native lock that valid reporters can reenter. Public observer values, detached-map identity and Go asynchronous-return adaptations need explicit limits. No new production API, runtime fix, Source execution, default-test skip, full-suite/profiling result or accepted divergence accompanies this baseline.
