# Codemode sandbox boundaries on pi v1.7.0

The unchanged `packages/codemode/test/sandbox.test.ts` at `fb0fe823539f6d04850f6e34748c01f01f212e1f` was verified from Git blob `d85c1e5d09c9ec89d54040eb39f21efe4ca6c512` and SHA-256 `a76f2e0f3688286fcb15de79a675cc05bae699117e5f740b12dd26dbb0090931`; the checked-out file and Git-object stream had the same SHA-256. Vitest 4.1.12 on Node 26.13.0 ran the byte-identical file unchanged under `/work/port-lanes/.v170-focused-verify.lock` with `NODE_OPTIONS=--max-old-space-size=650`: **156 passed, 0 failed, 0 skipped** in 17.92 seconds. This lane starts at native main `d557b0a1`.

## Bounded native comparison

The largest coherent exact subset is the 46 unique inline-backend cases for script values/errors, output and image admission, tools, store, globals, interruption/output limits, lifetime isolation and capability fences. The one inline case is the embedded QuickJS boundary repeated by the worker and remote host adapters, so their 96 process/RPC identity variants are not counted again as native claims. Existing `internal/quickjs` controls and public `durable.QuickJSSandbox` controls are mapped in the assertion ledger; none required a production change.

Two inline cases remain explicitly adapted rather than exact:

- `accepts wrapped and large base64 image data` accepts an over-1 MiB literal through the JavaScript hosts. The public native QuickJS setup cap rejects that identical literal; the native control constructs the same admitted image through a smaller program and checks MIME, size and digest. No setup-cap widening was made.
- `rejects execute after close` observes a rejected JavaScript Promise. Native exposes a closed-engine result error instead of host JavaScript rejection semantics. Both reject another execution with the `closed` boundary, but the envelope is deliberately not claimed identical.

The source-parse Node `vm.Script` control, worker-path/WASM/process failures, `--watch` handling, worker bridge packets and worker/remote transport identity remain unmatched. They measure Node/Bun, host files, Worker RPC or process behaviour rather than an embedded native QuickJS contract.

## Exact locators

- Manifest, 48 unique inline case ranges, adaptation flags and native test locators: `docs/upstream-v1.7.0/assertions/codemode-sandbox.jsonl`.
- Core public controls: `durable/codemode_sandbox_output_order_v160_test.go:TestSourceV160CodemodePublishedOutputOrder`, `durable/codemode_exit_v160_test.go:TestSourceV160CodemodeInlineEngineExit`, `durable/codemode_catchable_tool_error_v160_test.go:TestSourceV160CodemodeInlineEngineCatchableToolError`, `durable/codemode_unawaited_v160_test.go:TestSourceV160CodemodeInlineEngineCancelsUnawaitedCall`, `durable/codemode_store_snapshot_v160_test.go:TestSourceV160CodemodeStoreSnapshots`, `durable/codemode_sandbox_store_bounds_v160_test.go:TestSourceV160CodemodeSandboxStoreBounds`, `durable/codemode_sandbox_global_call_details_v160_test.go`, `durable/codemode_sandbox_global_admission_v160_test.go`, `durable/codemode_sandbox_namespace_missing_v160_test.go` and `durable/codemode_sandbox_stack_v160_test.go`.
- Embedded controls: `internal/quickjs/engine_120_test.go`, `tools_crosswalk_120_test.go`, `store_crosswalk_120_test.go`, `globals_crosswalk_120_test.go`, `isolation_crosswalk_120_test.go`, `abort_signals_v160_test.go`, `no_deadline_v160_test.go`, `output_limits_v160_test.go`, `sandbox_lifetime_v160_test.go`, `frozen_objects_v160_test.go` and `tool_aliases_v161_test.go`.

## Focused validation

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
flock -w 300 /work/port-lanes/.v170-focused-verify.lock go test ./internal/quickjs \
  -run '^(TestQuickJS120DeterministicClockRandomStoreAndTools|TestQuickJS120HardLoopCancellationAndHeap|TestQuickJS120PinnedToolAliasesListingsAndErrors|TestQuickJS120IndependentExecutionsAndRecursion|TestQuickJS120PinnedStoreMutationAndFailureReports|TestQuickJS120PinnedStoreLimitsAndRecovery|TestQuickJS120PinnedGlobalsAndFrozenNamespaces|TestQuickJS120PinnedIsolationCases|TestSourceV152NonFiniteOutputAndThrownValue|TestSourceV160QuickJSAbortSignals|TestSourceV160QuickJSFrozenObjects|TestSourceV160QuickJSNoDeadline|TestSourceV160QuickJSOutputLimits|TestSourceV160QuickJSSandboxLifetime|TestSourceV161QuickJSToolAliases)$' \
  -count=2 -timeout=90s
flock -w 300 /work/port-lanes/.v170-focused-verify.lock go test ./durable \
  -run '^(TestSourceV160CodemodeInlineEngineExit|TestSourceV160CodemodeInlineEngineCatchableToolError|TestSourceV160CodemodeInlineEngineCancelsUnawaitedCall|TestSourceV160CodemodeInlineNamespacedGlobals|TestSourceV160CodemodeStoreSnapshots|TestSourceV160CodemodeSandboxStoreBounds|TestSourceV160CodemodePublishedGlobalAdmission|TestSourceV160CodemodePublishedReservedStoreNames|TestSourceV160CodemodePublishedGlobalCallDetails|TestSourceV160CodemodePublishedNamespaceSpread|TestSourceV160CodemodePublishedMissingNamespaceMember|TestSourceV160CodemodePublishedOutputOrder|TestSourceV160CodemodePublishedInvalidOutput|TestSourceV160CodemodePublishedThrownObject|TestSourceV160CodemodePublishedNonStringErrorMessage|TestSourceV160CodemodePublishedNonFiniteLogging|TestSourceV160CodemodeScriptStacksAcrossPlacements|TestSourceV160CodemodeFailedScriptValuesAcrossPlacements|TestSourceV160CodemodeNonFiniteAcrossSandboxPlacements|TestSourceV161CodemodeStoreCheckpointAndFork|TestSourceV161QuickJSStartRetainsStrictAliases)$' \
  -count=2 -timeout=120s
flock -w 300 /work/port-lanes/.v170-focused-verify.lock go vet ./internal/quickjs ./durable
flock -w 300 /work/port-lanes/.v170-focused-verify.lock \
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./internal/quickjs ./durable
```

Both selectors pass twice with the 650 MiB Go memory limit. No production gap was established, so this receipt makes no speculative runtime change. No full Codemode/full-acceptance run, profiling gate, host process claim, hosted CI, root-worktree edit or push is part of the receipt.
