# Conversation watch acquisition and close

Two adapted scenarios from pinned `packages/durable/test/harness-view.test.ts:295–328` at `81f3cd412758ebdd92324c7d63b669a189408d0a` pass unchanged production in `durable/view_acquisition_oracle_main_test.go`. Literal terminal tags and error diagnostics still differ.

| Source assertion | Native check | Disposition |
| --- | --- | --- |
| Acquire, close Harness, await watch.closed = {kind:ended} | Acquired unstarted watch closes after public Harness.Close; End is session_closed without an error | Scenario ported; source literal ended tag remains unported |
| watch.value.entries equals [] after close | Value remains readable after close with a non-nil empty entries slice | Ported |
| Later root.watch rejects | Returns no watch and errors.Is(ErrClosed) | Rejection ported; native diagnostic vocabulary differs |
| Hold a commit on the Session line, queue acquisition, abort with Error(cancelled), release/await commit; queued acquisition rejects with cancelled | Same sequence through real Session.Commit, cancellation via context.WithCancelCause; no watch and errors.Is(context.Canceled) | Cancellation/rejection ported; source abort-cause diagnostic is unported |
| Hold another commit, queue acquisition, initiate close, release/await commit; acquisition rejects Harness is closed, then close completes | Same sequence; no watch and errors.Is(ErrClosed), then public Close joins | Rejection/ordering ported; literal diagnostic unported |

Native fixture initialises built-ins instead of the source chat/provider setup. Go calls run in goroutines where source calls execute synchronously until their first await. A context wrapper signals actual Done evaluation at the Session admission select; a goroutine-start signal alone would not establish that acquisition reached admission. The same wrapper signals Close's final select, which follows its synchronous closing flag. It delegates cancellation and Err unchanged. Held callbacks are released idempotently, including cleanup. No sleeps or scheduler-yield probes are used.

The native Session publishes session_closed/session_failed tags for both definition watches and raw commit subscriptions. ConversationWatch currently delegates End directly. Mapping only the conversation-facing terminal tag could preserve raw subscription provenance, but End/Stop consistency, sibling watch APIs and failure precedence need review before changing that API. Cancellation currently returns ctx.Err, losing a distinct context.Cause diagnostic; preserving errors.Is(context.Canceled) while exposing cause would also need a consistent admission-error contract. These differences are unresolved; the tests retain native expectations explicitly and do not establish acceptance of the divergence.

## Development checks

Go 1.27.3, GOMAXPROCS=4, project environment sourced:

- New tests, twenty repetitions/shuffle 42: CGO 1 passes in 1.196s; CGO 0 passes in 0.409s.
- New acquisition plus mount, history and listener-isolation tests, race three repetitions/shuffle 7: passes in 15.879s.
- Durable vet and staticcheck v0.8.2 pass in both CGO modes; module verification and diff checks pass.

Only tests/documentation changed. The unrelated continuation prototype remains untracked and excluded; it was temporarily moved for static checks and restored with a content comparison. Temporary upstream extracts and disposable scratch are removed after use. No full-suite/full-race, mutation, performance or acceptance-profiling check was performed. Terminal tag/cause/diagnostic equivalence, shared identity, general JavaScript timing and the remaining assertion-index rows stay open.
