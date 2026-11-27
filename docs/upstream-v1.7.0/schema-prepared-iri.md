# Prepared Schema distribution and absolute IRI checks

## Published archive bytes

The retained `@earendil-works/pi-schema` archives matched their recorded SHA-256 and npm SHA-512 integrity values:

| Version | Bytes | SHA-256 |
| --- | ---: | --- |
| 1.6.1 | 145,246 | `b3409305a532c0b2b69617befb197126d75d489310dfcc9c6e4d1da929a50476` |
| 1.7.0 | 145,254 | `37a41ff7758ce75da128176954d0c1b4db3892a5d476ec1649dcbdb51dfd313c` |

Listing each archive with `tar -tzf` and hashing each named member streamed by `tar -xOzf` reproduced identical paths and payload digests for all 43 executable `dist` members and all 172 `dist` members overall. No additions, removals or byte changes occurred under `dist`. Of four other package members, only `CHANGELOG.md` and `package.json` changed. The archive names, versions, exports, dependency declarations and engines matched their respective pinned source manifests; these checks establish archive identity and byte equality, not runtime loads or a commit attestation.

## Pinned source and native behavior

The Git source delta under `packages/schema` from v1.6.1 to v1.7.0 at `fb0fe823539f6d04850f6e34748c01f01f212e1f` changes only the changelog and manifest. The conformance runner, fixture loader, format registry, IRI predicate and fixture matched the Git bytes of both releases. The v1.7.0 lock matched its own pinned bytes. The retained `draft2020-12/optional/format/iri.json` fixture matches blob `db27c829a22f1c2eef26a80a85851562f2b0bbce`. In a private copy of the locked workspace stage, `node ../../node_modules/vitest/vitest.mjs run test/conformance.test.ts --maxWorkers=1 --reporter=dot -t '^draft2020-12/optional/format/iri '` from `packages/schema` passed all 52 unchanged originals on Node 26.13.0 with Vitest 4.1.12. The other 8,374 originals were outside this run.

Separate pinned `compile().check` and `compile().errors` captures confirmed the 26 verdicts and exact arrays: 17 valid and nine invalid, with one root `format` record reading `expected format iri` per invalid value. Compiling without formats accepted all 26 as annotations. On unchanged prepared Go at `4bbf033e360745c3f41bceaaec84f381d6c880db`, `AssertFormats: true` accepted all nine invalid vectors and emitted empty diagnostics. The fix and tests in `3e6e6aeb58cfd0f2c2f73c597d924eef892783e9` check the pinned syntax before using the existing WHATWG parser without a base. The first IPvFuture spelling is replaced for the check only, below the pinned limit of 2,048 UTF-16 units. `iri-reference`, default annotations, legacy checks and tools are unchanged. The predicate does not fetch files or use the network.

`TestSourceV170SchemaPreparedIRIFormatConformance` maps the pinned runner's validity and error-agreement comparisons across the 26 retained vectors and also compares exact arrays. `TestSchemaPreparedIRIFormatBoundaries` uses separate pinned probes, including cases immediately below and at the IPvFuture unit limit with ASCII and supplementary-plane characters. The focused root-package tests passed twice with `-shuffle=42`; `go vet .` and Staticcheck v0.8.2 passed. All Oracle and Go runs used the shared focused lock, and Go used the warm project cache. Five compiled controls failed as expected: dropping the character/percent guard, accepting relative paths, counting bytes or code points instead of UTF-16 units, and applying the new predicate to the legacy path. The evidence is limited to this fixture and the recorded probes.
