# Codemode deterministic clock, seed and call order on pi v1.7.0

The unchanged `packages/codemode/test/deterministic.test.ts` at `fb0fe823539f6d04850f6e34748c01f01f212e1f` was byte-verified from Git blob `cb5d7895a5b227a114731c7398e98c7774d5128c` with SHA-256 `7e052391ea766e58e34ae204c8b641f60a0eeaa47137f3247304c47d2847d912`. Vitest 4.1.12 on Node 26.13.0 ran the unchanged file under `/tmp/go-ai/v170-focused-verify.lock` with a 650 MiB heap: **30 passed, 0 failed, 0 skipped**. This lane starts at native `b1801c447d1193d56e3fadba2ac5100593b76813` and does not claim the worker or remote process boundaries.

## Bounded public comparison

Two neutral public `durable.QuickJSSandbox` controls compare the representable deterministic-start contracts: fixed start clock and pinned seed values, monotonic delivery time, identical replay calls/results, and call/result application in sequence rather than delivery-arrival order. The existing native clock test is updated to admit a lagging host timestamp while retaining the non-rewind projection.

Before the fix, both new controls failed through `Deliver` with `durable: rejected: invalid QuickJS parallel delivery`. The unchanged parallel adapter required sequence numbers to equal the next delivery and equated delivery arrival with VM application order. It also rejected a timestamp behind its previous accepted timestamp instead of retaining the maximum observed clock.

The bounded production change keeps per-call admission independent, queues validated deliveries by host sequence, releases only the next sequence into the VM, and advances the deterministic clock only when a callback is processed. A delivery sequence is accepted once; its validation, call identity and JSON ownership checks remain unchanged. On drain-enabled runs, acknowledgement is renewed at the drain boundary rather than per call in a parallel batch.

## Exact locators

- Original case/line locators: `docs/upstream-v1.7.0/assertions/codemode-deterministic.jsonl`.
- Public seed/clock/replay test: `durable/codemode_deterministic_v170_test.go:TestPublicCodemodeV170DeterministicSeedAndClockBoundaries`.
- Public ordered-delivery test: `durable/codemode_deterministic_v170_test.go:TestPublicCodemodeV170DeterministicCallOrderBoundaries`.
- Updated non-rewind control: `durable/codemode_quickjs_clock_120_test.go:TestCodemode120QuickJSClockAdvancesWithoutRewinding`.
- Existing pinned seed/environment control: `durable/codemode_deterministic_crosswalk_120_test.go:TestCodemode120PinnedDeterministicSeedsAndEnvironment`.
- Production boundary: `durable/codemode_quickjs.go` (`quickjsExecution`, `PrepareCall`, `Deliver`, `advanceQueuedDelivery`, `releaseQueuedDeliveryLocked`).

## Focused validation

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60 GOFLAGS=-p=1 GOTOOLCHAIN=go1.27.3
gofmt -w durable/codemode_quickjs.go \
  durable/codemode_quickjs_clock_120_test.go \
  durable/codemode_deterministic_v170_test.go
flock -w 300 /tmp/go-ai/v170-focused-verify.lock go test ./durable \
  -run '^(TestPublicCodemodeV170DeterministicSeedAndClockBoundaries|TestPublicCodemodeV170DeterministicCallOrderBoundaries|TestCodemode120PinnedDeterministicSeedsAndEnvironment|TestCodemode120QuickJSClockAdvancesWithoutRewinding)$' \
  -count=2 -timeout=90s
flock -w 300 /tmp/go-ai/v170-focused-verify.lock go vet ./durable
flock -w 300 /tmp/go-ai/v170-focused-verify.lock \
  go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./durable
```

The selector passed twice. `go vet ./durable`, Staticcheck v0.8.2, and `git diff --check` passed. No broader Codemode/full-acceptance run, profiling gate, hosted CI, process claim, or push is part of this receipt.
