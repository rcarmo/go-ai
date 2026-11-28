# Pinned Schema builder and TypeBox projection receipt

## Scope and source lock

- Native base: `d557b0a191976d8bf0c78e54488cf02162dfe062` (`main` at lane creation).
- Upstream target: `fb0fe823539f6d04850f6e34748c01f01f212e1f` (`v1.7.0`).
- Exactly these upstream files are owned by this lane:
  - `packages/schema/test/builders.test.ts`: SHA-256 `e5a3274c35c460d0e640d8fa24219788937375246744e97cfd87d245f2e9dae2`, Git blob `f87b27b41550d23a2eef19bc72ced004b7aa6511`.
  - `packages/schema/test/coerce-typebox.test.ts`: SHA-256 `3bdde8189ec5cf05e5bc8376d1def0d8bdfdaf6025098b4141d8bb5ed1220e85`, Git blob `fd28018641487f10cd394dde61c2f189d39362e5`.
  - `packages/schema/test/typebox-static.test.ts`: SHA-256 `6211182603cc9b548785be37bb399c8bb071ce9480fe38a84a7936eb8b60c377`, Git blob `c63c9268978f360db02929332db164899446f5bf`.
- Each working file matched the pinned Git object byte for byte before the run and remained unmodified.

## Pinned originals

The unchanged originals ran with Vitest 4.1.12 under the shared v1.7 focused-verification lock and a 650 MiB Node old-space limit:

```sh
bunx vitest run \
  packages/schema/test/builders.test.ts \
  packages/schema/test/coerce-typebox.test.ts \
  packages/schema/test/typebox-static.test.ts \
  --config packages/schema/vitest.config.ts
```

Result: **three files passed; 235 tests passed and two skipped**. The two skips are explicit TypeBox symbol-key/JavaScript-symbol cases and have no public JSON representation.

## Public native projection

`TestSchemaTypeBoxCoercionProjection` records the 229 conversion invocations in the 200 unique source case names. Each is compared through the public `CompileJSONSchema(...).Coerce` and optional `Check` paths after discarding TypeBox's symbol keys through its ordinary string-key JSON projection.

- 191 invocations map directly.
- Three invocations remain partial because the original deliberately calls private `convertOnly` (TypeBox pass 2); public `Coerce` also runs pi-schema's primitive type pass 3. The source result and the public result are both retained in the fixture and the public result is checked.
- 35 invocations remain partial because their input or result uses JavaScript `undefined`, `bigint`, or `-0` identity. They are retained as source records, not silently claimed as JSON parity.
- The original's two explicit symbol skips remain partial. TypeBox's symbol-keyed kind and optional/readonly markers are omitted from JSON projection exactly as the original test comment describes.

`TestSchemaBuilderJSONProjection` checks the emitted-schema semantics for plain objects, leaves, arrays, records, tuples, nullable/open objects, unconstrained unsafe schemas, bigint, recursive `$id`/`$ref` nesting and diagnostic ordering. `TestSchemaTypeBoxStaticJSONProjection` checks the corresponding runtime JSON projection. TypeScript-only `Static`, optional/readonly object shape and compile-time type narrowing have no Go generic counterpart and remain partial rather than being replaced by weaker runtime claims.

No semantic difference was demonstrated on the public native paths. Production code is unchanged.

## Local gates

Focused tests run twice with shuffle seed 42 under the shared `/work/port-lanes/.v170-focused-verify.lock`, `CGO_ENABLED=0`, `GOMEMLIMIT=650MiB`, `GOMAXPROCS=2`, `GOFLAGS=-p=1` and Go 1.27.3:

```sh
go test . \
  -run '^TestSchema(BuilderJSONProjection|TypeBoxCoercionProjection|TypeBoxStaticJSONProjection)$' \
  -shuffle=42 -count=2
```

`make vet` and Staticcheck v0.8.2 (`make staticcheck`) also passed under the same lock and resource ceiling. The scope excludes production changes, live provider calls, profiling, hosted CI, full acceptance, publishing and pushing.
