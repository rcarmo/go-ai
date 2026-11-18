# Canonical integration baseline

Latest complete run:a9a4018 plus the test-only public abort consumers.

Command: `make test PACKAGES=./... TEST_FLAGS='-v -count=1 -p 1 -timeout 1200s'`, after sourcing scripts/project-env.sh. CGO disabled, sequential packages. Baseline39 ran in a separate tmux session with a saved exit code. No overlays or test exclusions.

**Integration is red.** Complete baseline39: durable855.317s with32 failed top-level groups;25 other tested packages pass (tools15.527s/public12.464s). No panic, timeout, overlay or exclusions.89c031e remains the last fully verified tree.

Public abort ownership, outcome and late-usage consumers pass. [Mapping](canonical-builtin-execution.md#public-abort-ownership-results-and-usage).

Focused progress since baseline39: TestMainHarnessRecoveryCrashAfterConversion, TestMainInterruptedPollRetryEdges, TestMainPollTerminalOnlyBoundaryDerived, TestMainAbortedPollCancelHandlerCutoff and TestMainPollCutoffSkipsRepeatedCancellationDerived now pass. The table retains the actual sweep result; no lower full-suite count is inferred. Focused tests only until parity sweeps.

This is an observed failure inventory, not an assertion crosswalk or root-cause verdict. Names come from the complete run; links resolve source declarations.

| Test | Native file |
| --- | --- |
| `TestFailedRun140BoundedWithdrawalRecovery` | [failed_run_140_test.go](../durable/failed_run_140_test.go) |
| `TestFailedRun140WithdrawalJoinsOwnedHosts` | [failed_run_140_test.go](../durable/failed_run_140_test.go) |
| `TestMainBoundaryEligibilityInheritedCancellationBeforePlacement` | [harness_boundary_eligibility_main_test.go](../durable/harness_boundary_eligibility_main_test.go) |
| `TestMainBoundaryEligibilityAbsentAgentHostAllModes` | [harness_boundary_eligibility_main_test.go](../durable/harness_boundary_eligibility_main_test.go) |
| `TestMainBoundaryEligibilityLocallyDecidedFailFast` | [harness_boundary_eligibility_main_test.go](../durable/harness_boundary_eligibility_main_test.go) |
| `TestMainBoundaryLegacyRecoveryBusyControl` | [harness_boundary_lifetime_main_test.go](../durable/harness_boundary_lifetime_main_test.go) |
| `TestMainHarnessRecoveryCrashAfterConversion` | [harness_generation_recovery_main_test.go](../durable/harness_generation_recovery_main_test.go) |
| `TestM1bTerminalAndExplicitUnsupported` | [harness_generation_test.go](../durable/harness_generation_test.go) |
| `TestMainInterruptedBudgetAndCloseAccounting` | [harness_interruption_budget_main_test.go](../durable/harness_interruption_budget_main_test.go) |
| `TestMainInterruptedPollRetryEdges` | [harness_interruption_poll_main_test.go](../durable/harness_interruption_poll_main_test.go) |
| `TestMainPollTerminalOnlyBoundaryDerived` | [harness_poll_boundary_main_test.go](../durable/harness_poll_boundary_main_test.go) |
| `TestMainAbortedPollCancelHandlerCutoff` | [harness_poll_boundary_main_test.go](../durable/harness_poll_boundary_main_test.go) |
| `TestMainPollCutoffSkipsRepeatedCancellationDerived` | [harness_poll_boundary_main_test.go](../durable/harness_poll_boundary_main_test.go) |
| `TestM1cInvalidToolUsageSettlesOnceNoAppOrReplay` | [harness_tools_test.go](../durable/harness_tools_test.go) |
| `TestIdentity140DurableDefaultHostAndPinnedOverrides` | [identity_140_test.go](../durable/identity_140_test.go) |
| `TestRestartLimit140BuiltinGenerationAbortCutoffSettlesSubmission` | [restart_limit_140_test.go](../durable/restart_limit_140_test.go) |
| `TestStopBackgroundSuppressesReportAndChildStaysUsable` | [subagent_test.go](../durable/subagent_test.go) |
| `TestTaskRecoveryBuiltinHoldFinalFailureAndReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryToolOutcomePanicRollbackFallbackAndReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryGenerationPreparationFallbackWakeSequence` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryOfflineGenerationPreparationFallbackWakeSequence` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryToolDecidingHoldRejectedUncertainAndReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryQueuedWithdrawalOwnedChildDefersSettlement` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryGenerationDecidingFinalFailureMatrix` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryGenerationHostCallbacksPreparationAndDispatchedReceipts` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryHostPanicDecidingStoragePrefixAndReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryToolValidatorHostFailureBeforeIntentAndDecidingReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoverySuccessfulValidatorIntentStoragePrefixAndReopen` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskRecoveryOrderedToolContinuationAdmissionCloseAndLateOwnedWork` | [task_recovery_test.go](../durable/task_recovery_test.go) |
| `TestTaskSchedulerCapacityToolChildHoldOne` | [task_scheduler_test.go](../durable/task_scheduler_test.go) |
| `TestMainTaskTraceQueuedFirstInvocation` | [task_trace_main_test.go](../durable/task_trace_main_test.go) |
| `TestToolRecoveryPinnedCurrentAgentCwdAndDeselection` | [tool_selection_recovery_test.go](../durable/tool_selection_recovery_test.go) |
