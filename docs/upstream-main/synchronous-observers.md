# Synchronous observers remain unimplemented

The native watch API does not implement pinned upstream document observers. This gap remains open against `81f3cd412758ebdd92324c7d63b669a189408d0a`.

## Reviewed contracts

- `packages/durable/src/session/session.ts`, `observeDoc` and `#observeDocument`: attach at the admitted publication boundary, return the current value, follow the document address and deliver each change synchronously during publication, outside storage adoption. Retirement preserves the preceding value; recreation replaces it. Owner termination detaches the commit subscription and ends the observer.
- The same file, `guardObserver`: contain synchronous throws and thenable rejections from `advance`. Report the original error, detach, and call `end` with `listener_error` if still attached. Self-detach suppresses that end. Contain and report errors from `end` too; these guarded errors do not fail commits or the Session.
- `session-observers.test.ts`: queued/cancelled attachment, detach during publication, another observer detaching a later observer, retirement/recreation, listener-error containment, async rejection and lifetime tests remain unmapped to a native observer API.
- `harness-observers.test.ts`: normal close fails a synchronous observer with `session closed`, while a watch ends normally; actual Session failure carries its first cause. These are different callback/end contracts.

Fatal failures of lower-level synchronous Session listeners must remain separate from guarded observer errors. The initial review conflated those layers; reading `guardObserver` corrected that interpretation. No observer-close error should be promoted to a fatal Session failure merely to fit the existing native failure path.

## Native publication boundary

`prepareSessionPublication`, `settlePreparedPublication` and `publishSessionPublication` execute under the Session line. They prepare detached watch work, settle storage, adopt the published image and enqueue observers. `Session.commit` releases the line before making callbacks runnable. Existing document watches serialize callbacks off-line and fold pending changes; commit subscriptions are also asynchronous. Neither supplies synchronous, per-publication observer delivery.

Calling arbitrary Go observers under the Session line would block synchronous re-entry into Session APIs. Simply invoking them after `leaveThen` releases the line would let a queued later commit publish before the earlier observer runs. Reusing watches would additionally fold away individual publications and use different error/ending semantics. These alternatives have not established the target contract and are not shipped as an observer API.

The implementation needs a publication notification phase with explicit re-entry/admission semantics, stable per-publication delivery order, detach-safe listener iteration and guarded lifetime/error handling. It must preserve existing blocking APIs, admission, cancellation, first-failure fencing and Close behaviour. Review that boundary alongside [publication ordering](durable-group-contract.md); overlap remains disabled. A blocking-callback restriction or changed legacy ordering is not implicitly accepted.

This is a partial source review, not a complete disposition of `session-observers.test.ts`, a production implementation, or an acceptance result. No prototype was added.
