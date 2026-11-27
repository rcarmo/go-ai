# Codemode source parsing on pi v1.7.0

The five unchanged `packages/codemode/test/source.test.ts` cases now have a strict native translation for the exact `fb0fe823539f6d04850f6e34748c01f01f212e1f` source pin. The lane started from clean native `033fea39d5803fad264edca7e67670226be64b30` and selected this group because portable batching, worker lifetime and construction failure already have separate receipts.

## Exact original and implementation bytes

The test and implementation files were read from the target Git commit. Their object IDs and SHA-256 digests were checked independently:

| Path | Git blob | SHA-256 |
| --- | --- | --- |
| `packages/codemode/test/source.test.ts` | `36e7c0b7297dca32e6300e66b4be9abc51d5c10c` | `95fee0e41ece5f48851a430a933db1660c002034d25f3dd07a0003063c8eb3bb` |
| `packages/codemode/src/source.ts` | `6d7ba8573008646b811e8bf0ce323b5eb9a89adc` | `3938c223954116cd067de277bcf4ef0ff77b6f61986e2e746db82017b4b7bd36` |

The existing test's retained hash matches the recorded `source.test.ts` hash in `codemode-worker-construction.md`. No copied upstream TypeScript was added to the lane.

## Test-first result

`TestSourceV170CodemodeOriginal` failed twice on unchanged production code. Misplaced-option errors were `*durable.StorageRejected`, and empty-source errors were `*durable.StorageRejected` with `durable: rejected: codemode input is empty`. The original requires `CodemodeSourceError` and its source parser's exact message contract.

`durable/codemode_source.go` now exposes `CodemodeSourceError`, checks misplaced directives before parsing the first line, preserves line numbering, and returns the pinned source-class messages. `durable/codemode_options.go` distinguishes malformed JSON, a non-object value, unsupported fields and field-specific safe-integer or range failures. Its JSON decoder retains `json.Number`, so `1e9999` reaches the numeric-option validation path instead of being reclassified as invalid JSON. Omitted Go options still resolve to native `300000` ms and `10000` output tokens; the original observes an absent option field.

Two temporary controls failed as required:

- Disabling the misplaced-line guard admitted the case and produced `expected CodemodeSourceError, got nil`.
- Returning `StorageRejected` from the source-error helper failed the exact error-class assertion.

Both mutations and their scratch paths were removed before the final run.

## Focused validation

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOFLAGS=-p=1 GOMAXPROCS=2 GOMEMLIMIT=650MiB GOGC=60
go test ./durable \
  -run '^(TestSourceV170CodemodeOriginal|TestCodemode120SourceDirectiveAndUnicodeOutput|TestCodemode120JavaScriptOptionNumbersAndInvalidOutputBudget|TestCodemode120DeadlineAndReplayDoesNotReset)$' \
  -count=2 -shuffle=42 -timeout=120s
go vet ./durable
make staticcheck
git diff --check
```

The focused selection passed twice. Vet, Staticcheck v0.8.2 and whitespace checks passed. The existing opt-in official-package differential test was not enabled in this lane and no live external dependency was used.

The source implementation is still outside a full upstream-original Vitest execution in this lane. This receipt covers the five source cases through the native parser, not the declarations or deterministic groups, wider Codemode runtime, acceptance gates or release profiling.
