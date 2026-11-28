# Schema validator/coerce member-keyword receipt

## Scope and source lock

- Native base: `2d605e5b4b07c6680c5ffa5b86eb5a1b25270879` (the current `main` when this lane started).
- Upstream target: `fb0fe823539f6d04850f6e34748c01f01f212e1f` (`v1.7.0`).
- `packages/schema/test/validator.test.ts` is already represented by the native validator and compiled-schema suites. This packet therefore uses the smallest adjacent deterministic validator/coerce source, `packages/schema/test/coerce.test.ts` (514 lines, compared with 530 for `validator.test.ts`).
- The source file was byte-checked at SHA-256 `13794a83a21c67b24c5647232b1d850be4b49c15acfb3efc303d1678634f8092` and Git blob `b264a442d9105ed53f1a3bb88dfd8a404886da7a`.
- Neutral locator: the `member keywords across the passes` table, especially pattern/additional members, node properties beside `anyOf`, and `anyOf`/`oneOf` candidates after node properties.

## Original verification

The unchanged original passed locally with:

```sh
bunx vitest run packages/schema/test/coerce.test.ts --config packages/schema/vitest.config.ts
```

Result: one file, 49 tests passed, no failures or skips.

## Native production-path comparison

`TestSchemaCompiledMemberKeywordsStrictAndCoerced` uses only the public `CompileJSONSchema` API. For each source case it compares:

- strict `Check` and exact `Explain` diagnostics with the unchanged value;
- `Coerce` and `Parse` outputs, followed by successful strict `Check` and empty `Explain`;
- immutable input bytes/values across coercion.

No production mismatch was demonstrated. The packet adds only deterministic coverage and this receipt; it does not change schema defaults, tool coercion policy, reference handling, format policy, or existing bounded limits.

## Local gates

The commands below ran under `/tmp/go-ai/locks/next-schema-validator-shared.lock` with `CGO_ENABLED=0`:

```sh
go test . -run '^TestSchemaCompiledMemberKeywordsStrictAndCoerced$' -shuffle=42 -count=2
make vet
make staticcheck
```

All passed. `git diff --check` also passed. No full acceptance, profiling, hosted CI, push, or root-worktree validation is included.
