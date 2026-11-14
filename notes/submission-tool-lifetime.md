# Submission tool, trace and lifetime packets

Target: official pi-durable v1.5.0 at `f9a26365568127461c47a07a37dcb42c140a3ac5`.

Three literal source packets now run through real Harness tools and SQLite:

| Pinned test | Native test | Result |
| --- | --- | --- |
| harness-submit-in-commit286–310, model-visible queued write | TestSubmitInModelVisibleWriteQueueAndPlacement | Holds the actual tool, checks the canonical Inbox draft, releases the tool and verifies both input completion and the placed write record/entry. The stronger fixture passed unchanged runtime. |
| harness-telemetry341–380, steer trace | TestSubmissionSteerLinksRunningTraceWithoutReparenting | Steer span is a root linked to the running task carrier; later phases do not use the steer carrier as their parent and are ended at idle. Missing link failed before the fix0.569s. |
| harness-ownership1013–1075, bound child conversation | TestSubmissionToolBoundConversationLifetime | Tool creates/configures a task-owned child, submits and waits. Missing conversation returns no handle without error. Ended submit/idle/abort/submission-wait operations reject; exactly one child user entry, no active child tasks and a durable done submission remain. Missing lookup failed before the fix0.885s. |

The source child configuration call is represented by the existing production agent configuration encoder and document operation inside the tool commit. Tool output uses Go's text-result field; no executor, handle or settlement is stubbed.

Harness.Conversation now returns `(nil, nil)` for an absent record. TaskRuntime preserves the same absence after its second lifetime check, without wrapping a nil host handle. Pinned harness/harness.ts404–407 and the ownership test establish this contract. The root-bootstrap regression now asserts no handle/no error before Root creates conversation1; its record-count, real-commit and reopen assertions are retained.

Steer admission captures an owned copy of the current run's task carrier after deduplication and before table writes. Standalone admission extracts and links it before ending its submit span, after leaving the admission locks. Carrier extraction callbacks remain off the Session line. The source invokes link extraction inside admission; complete in-flight callback timing and caller-owned submit tracing still need their own assertion audit.

## Verification

Full26 tested packages pass: durable838.557s, tools28.232s, public consumers12.387s. The earlier full571.063s failed the obsolete root-bootstrap missing-lookup error assertion; it is not a passing receipt. A first lifetime fixture did not compile because it used the wrong Go result field/type; that receives no semantic-failure credit.

Ten shuffled repetitions pass: steer trace9.393s and bound lifetime11.248s. Three tool-bound placement repetitions pass1.794s. Existing submission trace controls pass0.793s. Three independently compiled faults restore the missing-lookup error, wrap an absent host handle, or omit the run link; all fail the new assertions. Vet, Staticcheck v0.8.2 and logging pass.

Unmodified original files pass with all87 supplemental cases: submit-in-commit110 cases, telemetry113, ownership128; zero skipped/todo/filtered cases. Original execution establishes the reference behaviour, not acceptance of every corresponding native assertion. Analysed temporary scripts, overlays and logs were removed.

## Next boundary

Canonical GenerationTask2 and ToolTask3 production execution and migrations are incomplete. Existing normal execution still uses private builtin checkpoints; canonical publication projections do not establish imported-record execution. Arbitrary/uncertain Storage failure, task-bound queued close/seal precedence, full trace callbacks/events and exhaustive lifecycle/resume order also need acceptance. Full alignment, the complete original oracle and pre-release profiling remain incomplete.
