# Schema validation oracles at b556cfe5

Three optional-null probes from the new upstream tip pass after a bounded native normalisation fix. The rest of the new validation corpus needs review. Rui's current direction is focused tests only, fidelity first and no profiling until the baseline is established.

## Exact source port

Target: `b556cfe5b0300a91f39c7bc5369a65a25fb7fcd5`, `packages/ai/test/validation-probes.test.ts:735–750`, “reviewer probes (round 2)”. Native tests: `tests/upstream_validation_main_test.go`, `TestUpstreamMainValidationOptionalNullPropertyValues`.

All three cases preserve the source schema, input and result:

- Item schema: object with optional `n: {type: "integer", minimum: 1}`.
- Input: `{entry: {n: null}}`.
- Root declares `entry`, supplies the item through `additionalProperties`, or matches it through `patternProperties: {"^entry$": item}` with `additionalProperties: false`.
- Expected result in each case: `{entry: {}}`.

Adaptations are JSON schema bytes in the Go `Tool` and direct semantic map comparison instead of the JavaScript `outcome()` string. Calls use public `ValidateToolArguments`. No test helper replaces the normaliser or validator. The declared case passes unchanged production; additional and pattern cases fail because native traversal only follows named properties. These source assertions were inspected and translated; the upstream Vitest suite was not executed.

## Ownership and property controls

`validation.ts` parses a `structuredClone` of arguments; native normalisation previously shallow-copied the root, then deleted fields from caller-owned nested objects. A source-derived ownership supplement checks object, homogeneous-array and tuple-item paths. All three fail with the preceding production code and pass after the fix. Semantic input comparisons preserve null and empty-container distinctions.

Additional native controls check declared and matching-pattern schemas together, exclusion of their keys from `additionalProperties`, boolean pattern schemas, nullable/required/unmentioned properties and retention of null-valued pattern/additional keys themselves. These controls supplement the three source tests; they are not extra translated upstream cases.

`context.go` now returns owned containers along traversed normalisation paths and visits pattern/additional values for their own optional declared nulls. Pattern matching compiles each expression once per visited object. Unsupported RE2 patterns conservatively suppress additional-property normalisation at that object. This preserves existing permissive handling outside the supported pattern domain; it does not implement ECMAScript regex validation. No public API or dependency changed.

## Focused development evidence

Go `1.27.3`, `GOMAXPROCS=4`, with `scripts/project-env.sh` sourced:

- Unchanged-production run of the new probes: additional and pattern fail; declared passes. The ownership supplement exposes mutation.
- Previous-production overlay, using both logical and physical Go file paths: fails both source regressions, all three named ownership cases and the combined property-boundary control. The overlay and isolated scratch were removed after analysis.
- Focused new and existing upstream-validation tests: ten normal repetitions per CGO mode, shuffle 42/7, pass (0.028/0.012 seconds); focused race five repetitions, shuffle 7, passes (1.052 seconds). Vet and staticcheck v0.8.2 for `.` and `./tests` pass both CGO modes; module verification passes. Final runs including the boolean-pattern control: ten normal repetitions per CGO mode, shuffle 42, pass (0.016/0.011 seconds); focused race five repetitions, shuffle 42, passes (1.047 seconds). Vet/staticcheck for the same two packages pass both modes after that control.
- Before Rui reiterated focused-only validation, the `./tests` package ran once per CGO mode (5.161/5.392 seconds); this was not a full repository suite. The subsequent runs are filtered validation tests only.
- Inventory path/status/manifests and semantic regeneration independently match exact Git objects: 427 baseline package paths, 88 incremental package paths, 543 support paths, 362 target test/fixture paths and 210 schema dependency-test paths. An initial verifier stripped a trailing empty TSV evidence column with `trimEnd`; preserving trailing tabs corrected the verifier. The verifier fix did not change generated tables. A later staged whitespace check exposed empty final evidence fields as trailing tabs; the generator now writes `-` for missing evidence, preserving five columns without trailing whitespace.

No full repository tests, full race suite or profiling ran for this checkpoint. Earlier receipts keep their original pins and workloads.

## Open fidelity work

The source's `anyOf`/`oneOf` branch-plus-sibling conversion, recursive refs/unions and work bounds, 10,000-member enum diagnostics, formats, schema-object cache/mutation identity, non-JSON types and the rest of the probe matrix are unported here. Full structured-clone equivalence, including returned aliases on unvisited paths and unsupported Go value types, is also open. Existing native constraint/coercion/diagnostic limitations are unchanged outside this normalisation slice.

The new durable argument-type test requires a native typed-tool API mapping. The schema dependency has its own 210-path review queue; inspecting its source is not a passing dependency test suite. Review sources and translate assertions before changing more production behaviour.

The earlier generation persistence oracle still fails: four frames/four stored touches versus source `touches + 1`. Its generation/live/view sources and harness-view test are unchanged at this pin. See [generation oracle](generation-view-oracle.md).
