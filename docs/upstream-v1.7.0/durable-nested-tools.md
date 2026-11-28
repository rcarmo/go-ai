# Unchanged Durable nested tool, child, gate and hook originals

This bounded packet owns the five exact pi v1.7.0 Durable originals below at `fb0fe823539f6d04850f6e34748c01f01f212e1f`. The isolated native lane starts at `d557b0a191976d8bf0c78e54488cf02162dfe062`; the tested runtime candidate is `5b9bba574dac8bcb145dbcdd833134fbf5001631`. It covers public child/task/tool ownership, restart and keyed admission, parked children, gate suspension, hook effects and cancellation. It does not overlap unrelated Durable, provider or Codemode work.

## Pinned originals

| File | Git blob | SHA-256 | Result |
| --- | --- | --- | ---: |
| `harness-nested-tools.test.ts` | `6792aec940db386385eef4a24cf8e71ee6491ae1` | `14a76ec87cd187e7a222f30d6a2132b307a75c9b10a6c3bd0b6f168295d94587` | 35 |
| `harness-nested-tools-restart.test.ts` | `2acf1495e68b04664f004edf3112e48386c42fd7` | `d7fde5c0423cb5f2339ad056ecdc629db37d897c7c5f0e6d7e2133ba7332e843` | 11 |
| `harness-park-children.test.ts` | `cd71af91e4dcb1813f12163d37830d1b89f414c0` | `7b8bb087558b1da2a33a7a02e775c50941ac943f6d62916e41e5211a5eb20a08` | 10 |
| `harness-tool-gates.test.ts` | `d5b6967bcad9542fc6e8edd51d4d13bf093022a2` | `404931d676741d71db272f3b4240a43f578b1bd7aee592c75d210f696d0895e0` | 9 |
| `harness-tool-hooks.test.ts` | `80cfbf182fb90ffacafffa41791aed3b768866a5` | `a2fdc36eb7ffd4d27e00b3023d5ba8ffeb1b5ba376036b0470f1438eadc54ce4` | 18 |

Each staged file matched the pinned Git blob and SHA-256 byte for byte. The checkout remained detached at the pin. Node 26.13.0 and Vitest 4.1.12 ran all five unchanged files with `NODE_OPTIONS=--max-old-space-size=650` and the shared `/work/port-lanes/.v170-focused-verify.lock`: **83 passed, 0 failed, 0 skipped**.

```sh
cd "$PI_ORIGINAL/packages/durable"
NODE_OPTIONS=--max-old-space-size=650 flock -w 1200 /work/port-lanes/.v170-focused-verify.lock \
  node ../../node_modules/vitest/dist/cli.js --run \
  test/harness-nested-tools.test.ts test/harness-nested-tools-restart.test.ts \
  test/harness-park-children.test.ts test/harness-tool-gates.test.ts \
  test/harness-tool-hooks.test.ts
```

## Native comparison and fix

The unchanged native implementation was tested through the public Harness, Conversation, task, document, tool, wake and hook paths over SQLite. Existing neutral controls cover keyed result ownership, independent nested task IDs, parent attribution, cancellation, replay-safe restart reattachment, restart abandonment, missing definitions, parked children, shared task/call keys, submission notifications, gate wakes, output truncation and hook cutoff/restart boundaries.

The new `TestToolHookOwnsUnavailableCallAndControlsRound` failed twice before the fix. An unoffered call correctly owned a `pi.tool` version 4 task and ran `afterTool`, but a hook that replaced `Content` while retaining the detached empty `Blocks` arm lost its output from the model receipt. Hook `terminate` still controlled admission, so this was a result-composition defect rather than a task-ownership or hook-dispatch gap.

`settleCanonicalTool` now composes returned `Content` before explicit `Blocks`, matching the public `ToolResult` contract and the established noncanonical tool path. Slot rebuilds carry retained output only in `Content`, preventing duplicate progress text. The existing unoffered-task assertion now checks canonical v4. The fix preserves limits, diagnostics, error flags, image representation, task ownership and round control; it does not relax disclosure or resource limits.

## Locators

All 83 source cases and 306 direct `expect` locators are in [the assertion ledger](assertions/durable-nested-tools.jsonl). Every locator is a partial representative mapping because the native evidence uses public typed records and SQLite rather than JavaScript private helper identity or Chord object references. The manifest pins all five original SHA-256 values and names the native test used for each behavior family.

## Verification

The focused native selector ran twice with `-shuffle=42` under Go 1.27.3, `CGO_ENABLED=0`, `GOMEMLIMIT=650MiB`, `GOMAXPROCS=2`, `GOGC=60` and `GOFLAGS=-p=1`:

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMEMLIMIT=650MiB GOMAXPROCS=2 GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
flock -w 1200 /work/port-lanes/.v170-focused-verify.lock \
  go test ./durable -run '^(TestNestedTool120.*|TestNestedUTF16Main.*|TestSourceV160Nested.*|TestSourceV161Nested.*|TestSourceV160BeforeToolGate.*|TestMainCutoffHooks.*|TestSourceV160AbortHook.*|TestSourceV160ParkedToolKeepsNestedChildren|TestSourceV160ToolChildKeys|TestSourceV160ParkedChildAbandonedOnCallerRestart|TestSourceV160SubmissionNotifications|TestToolControlsUnavailableOrThrowingCallCannotTerminateRound|TestToolHookOwnsUnavailableCallAndControlsRound|TestSourceCanonicalToolModelExecutionAndMigration)$' \
  -count=2 -shuffle=42
```

The selector passed. `go vet ./durable` and Staticcheck v0.8.2 `./durable` also passed with no output. The generated crosswalk passes `bun docs/upstream-v1.7.0/test-crosswalk.ts --check` with the five rows populated. No full-suite, acceptance, profiling, live-provider or release result is implied.
