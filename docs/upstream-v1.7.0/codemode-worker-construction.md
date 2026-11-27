# Codemode construction-failure admission

The six v1.7.0 Codemode originals at `fb0fe823539f6d04850f6e34748c01f01f212e1f` were byte-verified in `/tmp/go-ai/runs/durable-port/pi-fb0fe823539f6d04850f6e34748c01f01f212e1f` against their pinned Git objects. The selected `packages/codemode/test/worker-lifetime.test.ts` bytes are SHA-256 `bbfd2ed4acbe7ceef8eeeae54f304dfdaba1a999e7e860f45bcb86059509d02e`. The retained source-run totals remain 231 unchanged originals: 19 declarations, 30 deterministic, 7 portable, 156 sandbox, 5 source and 14 worker-lifetime cases.

The exact original named **releases admission when worker construction fails** passed alone on Node 26.13.0 with Vitest 4.1.12 (1 selected pass; 13 filtered). It reserves worker capacity, supplies an invalid worker URL, observes a sandbox start error, and verifies exactly one acquisition and release.

Native coverage is `TestSourceV170CapacityReleasedOnModuleConstructionFailure` in `internal/quickjs/worker_lifetime_130_test.go`. The Go engine first compiles its WASM once; the test closes that precompiled module so the execution's actual `InstantiateModule` call fails. It verifies, through the production Engine execution path, that the capacity callback runs exactly once, the permit release runs exactly once, and `OnWorkerStarted` does not run. The Go result remains a sandbox-class `Failed to start QuickJS:` error rather than the original Node/V8 Worker message.

This comparison is limited to the representable ordering contract: admission happens before module construction, failed construction cannot enter the started hook, and reserved capacity is returned. It is not a Node Worker URL, Worker-thread ownership, Bun termination, or process-exit claim. The original WASM-load rejection is also not portable as written: native QuickJS is embedded and compiled while `NewEngine` is constructed, before an execution can acquire worker capacity; there is no per-execution WASM promise to reject. Those cases remain outside this receipt.

A temporary Go overlay removed only the deferred `release()` call. The selected native test then failed at `released=0; want 1, 1 and 0`, so the control detects the permit leak. The overlay and log were disposable and removed after the check.

## Focused validation

```sh
# exact original; all 13 other cases filtered
node node_modules/vitest/vitest.mjs run packages/codemode/test/worker-lifetime.test.ts \
  --pool=forks --maxWorkers=1 \
  --testNamePattern='releases admission when worker construction fails'

# native; unchanged production implementation
source scripts/project-env.sh
export CGO_ENABLED=0 GOFLAGS=-p=1 GOMAXPROCS=1 GOMEMLIMIT=512MiB GOGC=40
go test ./internal/quickjs \
  -run '^TestSourceV170CapacityReleasedOnModuleConstructionFailure$' \
  -count=2 -timeout=90s
go vet ./internal/quickjs
staticcheck ./internal/quickjs
```
