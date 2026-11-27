# Codemode construction-failure admission

The six v1.7.0 Codemode originals at `fb0fe823539f6d04850f6e34748c01f01f212e1f` were byte-verified in `/tmp/go-ai/runs/durable-port/pi-fb0fe823539f6d04850f6e34748c01f01f212e1f` against their pinned Git objects. The retained source-run totals remain 231 unchanged originals: 19 declarations, 30 deterministic, 7 portable, 156 sandbox, 5 source and 14 worker-lifetime cases.

| Original | Git blob | SHA-256 |
| --- | --- | --- |
| `declarations.test.ts` | `df981337b7a150f4b9a393ac652e7e257ddd24ca` | `fa84187ee4b2a01a2055060ac3725f240c452f7ad0a0b0b38071c67b35498bb5` |
| `deterministic.test.ts` | `cb5d7895a5b227a114731c7398e98c7774d5128c` | `7e052391ea766e58e34ae204c8b641f60a0eeaa47137f3247304c47d2847d912` |
| `portable.test.ts` | `ee4adb2a2568eb601649d8760c55f1e684c5df21` | `e80323f38106e4f63590a7ff727965c3b450422b95f6de679d9960b7d9db784f` |
| `sandbox.test.ts` | `d85c1e5d09c9ec89d54040eb39f21efe4ca6c512` | `a76f2e0f3688286fcb15de79a675cc05bae699117e5f740b12dd26dbb0090931` |
| `source.test.ts` | `36e7c0b7297dca32e6300e66b4be9abc51d5c10c` | `95fee0e41ece5f48851a430a933db1660c002034d25f3dd07a0003063c8eb3bb` |
| `worker-lifetime.test.ts` | `67ac4b2bb62c72566e7cbaf803467fd3b30a3a96` | `bbfd2ed4acbe7ceef8eeeae54f304dfdaba1a999e7e860f45bcb86059509d02e` |

The exact original named **releases admission when worker construction fails** passed alone on Node 26.13.0 with Vitest 4.1.12 (1 selected pass; 13 filtered). It reserves worker capacity, supplies an invalid worker URL, observes a sandbox start error, and verifies exactly one acquisition and release.

Native coverage is `TestSourceV170CapacityReleasedOnModuleConstructionFailure` in `internal/quickjs/worker_lifetime_130_test.go`. The Go engine first compiles its WASM once; the test closes that engine's runtime so the execution's actual `InstantiateModule` call fails. (Closing wazero's immutable compiled-module handle only evicts its compilation-cache entry and does not make later instantiation fail.) It verifies, through the production Engine execution path, that the capacity callback runs exactly once, the permit release runs exactly once, and `OnWorkerStarted` does not run. The Go result remains a sandbox-class `Failed to start QuickJS:` error rather than the original Node/V8 Worker message.

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
