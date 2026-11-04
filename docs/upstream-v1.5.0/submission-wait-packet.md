# Submission wait and close ordering

Pinned `packages/durable/test/harness-submissions.test.ts` contains 11 tests. This packet isolates three wait assertions; the other eight remain separate review work.

| Source | Assertion | Native boundary and present evidence |
| --- | --- | --- |
| 134 | Cancelling one wait leaves the submission pending; closing rejects a pending wait. | `SubmissionHandle.wait` in `durable/submission.go:407` registers waiters through `submission_status.go`. `TestSubmissionWaitRegistersAndSettlesFromArbitraryCommitWithoutPolling` covers one cancelled waiter alongside a successful second waiter. `TestSubmissionPendingPublicationWaitCloseAndPoisonRemoveRegistrations` covers close and failed Session cleanup. Compare full source assertions before calling either an exact port. |
| 149 | After reopening, hold the backend submission read, start close, release the read and require the wait to reject. | The pinned fixture holds a storage read before it returns. Native Session uses its snapshot/readTasks path, and native `Storage` wrapping restrictions complicate the same hold point. Existing registered-waiter close and invocation-sealing controls do not establish this race. Locate a production-path read seam and a close-start witness; do not model an already-registered waiter as the held read. P02 close runtime changes remain paused. |
| 284 | A wait resumes only after `observeCommits` has seen the settling commit. | The source uses its synchronous host commit observation. `durable/session_sync_observers.go` currently rejects scheduler-backed Sessions, while `SubscribeCommits` is asynchronous. `TestSubmissionWaitRegistersAndSettlesFromArbitraryCommitWithoutPolling` checks settlement, not host-observer ordering. The source assertion is an API/ordering gap; an asynchronous subscription or private listener alone cannot establish source parity. |

## Next bounded tests

Port source134 through the public submission surface with independent waiters, cancellation and pending status assertions. Port source149 only with a real read hold and witnessed close start; release holds and join workers on every failure. For source284, first port the source host-commit observer interface and its scheduler-backed dispatch, then observe the exact settling submission and assert the wait continuation sees it. Keep source JS scheduling differences explicit and verify no Session re-entry from observer callbacks.

Use the protected-prototype overlay and focused pure-Go tests. Preserve `TestSubmissionWaitRegistersAndSettlesFromArbitraryCommitWithoutPolling`, `TestSubmissionPendingPublicationWaitCloseAndPoisonRemoveRegistrations` and `TestInvocationSubmissionQueuedStatusAbortAndWaitRejectAfterSeal`; inspect their complete bodies before reusing them. This is a source inventory, not an execution receipt, and does not authorise the paused P02 runtime changes or prove full submission parity.
