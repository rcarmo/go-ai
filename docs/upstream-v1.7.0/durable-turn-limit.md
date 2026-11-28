# Durable turn limits in the unchanged v1.7.0 session

This bounded packet covers the deterministic SQLite behavior in the unchanged `packages/durable/test/harness-turn-limit.test.ts` suite at `fb0fe823539f6d04850f6e34748c01f01f212e1f`. It is separate from the session-batch, blocked, cancellation and harness-close ledgers. The isolated lane starts at exact native main `2d605e5b4b07c6680c5ffa5b86eb5a1b25270879`.

## Original and public behavior

The original Git blob matched the staged file byte for byte before and after execution: SHA-256 `be19144ced89e96c7ed4ece549f02f922be64386cf9e9cf5c1b7b3e3ae235f9d`, Git blob `18ec8c5ae3718d03dcd4ad6aaf7ad2cd64e30486`. Node 26.13.0 and Vitest 4.1.12 ran all 11 cases with no failures, skips or todos. The cases cover tool-round cutoff, fresh run and yield budgets, joined steer settlement, failed-run policy, live edit and clearing, integer validation, termination and handoff, manual continuation, child/fork inheritance and SQLite restart recovery.

The unchanged native SQLite/public-path tests passed all nine neutral Go groups. The test-first run compared cutoff and failure settlement, task completion, transcript order, live configuration, ownership of child conversations, failed-run handling and stored turn recovery. No production mismatch was demonstrated; no durable production file was changed. Consequently no scheduler, cancellation, ownership, storage or redaction contract was relaxed. The restart witness retains the real running tool owner and generation through a process kill instead of adapting the JavaScript storage proxy into a native envelope.

## Locators and limits

The native locators are `durable/harness_turn_limit_test.go` and `durable/harness_turn_limit_recovery_test.go`; the edits in this packet only retarget their source-pin comments to v1.7.0. Their tests already use behavior-level names and real SQLite paths, so new version-named duplicate tests would not improve coverage. The assertion ledger records every direct `expect` site from the 11 unchanged cases and maps it to these neutral groups.

Go's typed `int` configuration rejects non-integral and non-finite values at the type boundary, while the original exercises JavaScript `RangeError` for those runtime values. The native tests therefore preserve the representable nonpositive checks; that is an API-shape difference, not a demonstrated public behavior mismatch.

## Reproduction

```sh
# Pinned original, from its exact checkout.
sha256sum packages/durable/test/harness-turn-limit.test.ts
cd packages/durable
../../node_modules/.bin/vitest --run test/harness-turn-limit.test.ts --reporter=verbose

# Native lane.
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
flock -w 300 /tmp/go-ai/v170-focused-verify.lock \
  go test ./durable -run '^TestTurnLimit' -count=1
```
