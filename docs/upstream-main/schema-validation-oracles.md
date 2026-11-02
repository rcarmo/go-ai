# Schema validation oracles

Three optional-null probes at `b556cfe5`, two branch-plus-sibling probes, the depth-40 recursive-reference rejection and the 10,000-value enum oracle at `c9f4c7b9` pass after bounded native fixes. Selected numeric-bound probes and fresh-schema controls also pass. The rest of the new validation corpus needs review. Rui's current direction is focused tests only, fidelity first and no profiling until the baseline is established.

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

## Branch-plus-sibling checkpoint at c9f4c7b9

`tests/upstream_validation_union_main_test.go` ports the exact `validation-probes.test.ts:752–765` schemas, input `{a: "3", c: "2"}` and expected `{a: [3], c: 2}` for both `anyOf` and `oneOf`. Calls use public `ValidateToolArguments`; JSON-value comparisons adapt Go's decoded/coerced number representations without changing values or array structure.

Both source cases fail unchanged native code: `c` converts but `a` stays a string. Native source-derived controls also expose missing branch checks, accepted absent requirements, skipped nested conversion and ambiguous `oneOf` acceptance. These controls supplement the two translated assertions; they are not additional upstream test bodies.

The fix applies conversion to the root schema, separates conversion from non-coercing membership checks, converts object/array members and sibling properties, and checks each candidate against the whole union. Existing matches skip branch conversion; otherwise branches are tried in order. `oneOf` requires exactly one accepted branch. Failed candidates cannot mutate later candidates or caller input. Tuple schemas do not wrap scalars. References, unsupported keywords and complete validation remain open below.

`tests/upstream_validation_union_candidates_main_test.go` adds whole-node selection controls: one candidate can match both `oneOf` branches and must be skipped for a later valid candidate; a partially invalid conversion may instead satisfy another branch after sibling conversion. These six candidate scenarios were independently checked against the actual upstream compiler/validator. Primitive-typed non-object branches must not walk object members. Native numeric controls preserve already-valid `int64`, `json.Number` and `float64` values, including MaxInt64 and 9,007,199,254,740,993. An intermediate implementation lost wide values through float64; preserving valid typed values before conversion fixes that regression. These are exact native numerical-contract controls, outside JavaScript's wide-number representation domain.

Focused Go `1.27.3`/`GOMAXPROCS=4` evidence:

```sh
. scripts/project-env.sh
export GOTOOLCHAIN=go1.27.3 GOMAXPROCS=4
filter='^(TestUpstreamMainValidation|TestUpstreamValidation|TestTool130Argument|TestValidateTool)'
CGO_ENABLED=0 go test ./tests -run "$filter" -count=10 -shuffle=42 -timeout=90s
CGO_ENABLED=1 go test ./tests -run "$filter" -count=10 -shuffle=7 -timeout=90s
CGO_ENABLED=1 go test -race ./tests -run "$filter" -count=5 -shuffle=42 -timeout=90s
```

Final runs pass in 0.024/0.020/1.095 seconds. Vet and staticcheck v0.8.2 for `.` and `./tests` pass both CGO modes; module verification and diff checks pass. Previous-production overlay fails both exact source cases. Four additional overlays are rejected: skipped sibling conversion (both source cases), branch-only candidate acceptance (`oneOf` whole-node control), borrowed candidate maps (both later-branch controls), and removed valid-number preservation (numeric control). Overlays use both logical/physical paths and were removed after analysis.

The two original selected upstream bodies and helper were executed unchanged against actual pinned `validation.ts` and pi-schema sources under Bun, using only assertion shims and package-export adapters. Both pass, as do the six source-derived candidate scenarios. This is not the full Vitest suite. An initial resolver-adapter probe failed before loading the module; local package-export adapters corrected that harness issue without changing upstream code. Extracted sources, adapters and probe artifacts were disposed after each run. No full repository/full-race run, profiling or performance claim.

## Recursive-reference checkpoint at c9f4c7b9

`tests/upstream_validation_recursive_main_test.go` preserves the exact source schema, 40 nested `{c: value}` objects around `{x: 1}`, public validation call and outcome `REJECT - s.a: missing` from `validation-probes.test.ts:767`. The Go test adapts the source `outcome()` exception handling to an error return; its rejection prefix and first diagnostic remain literal. Unchanged `faef815` accepts the invalid input. Six of the initial eight source-derived reference controls also fail: four retain numeric strings and two accept invalid references. Missing optional unresolved references and true-schema targets already pass.

`schema_validation.go` now resolves document-local JSON pointers, including root, array-index, URI-encoded and escaped segments. Conversion and non-coercing checks follow referenced targets. The per-call caches use schema/value identity, retain inputs against GC/address reuse, and never share results across validation calls. Conversion re-entry leaves the value unchanged; results spanning a cutoff are not memoised. Declared, matching-pattern and additional-property conversion/checking follow their applicable schemas. Supported object/reference boolean schemas and string-enum reference siblings have controls. Unknown resources reject locally without fetching files or URLs.

The 16 JSON controls in `tests/upstream_validation_references_main_test.go` pass the actual pinned upstream validator/compiler as well as native code. They supplement the original source test. The original upstream depth-40 body and its helper also pass unchanged under Bun with assertion and package-export adapters; this is not the Vitest suite. An initial runtime module-resolution adapter failed before testing; package-export wrappers corrected it. A proposed rejection of literal `~2` pointer text disagreed with upstream, which preserves it; the fixture and resolver now follow the measured source result.

Native supplements cover input ownership, fresh calls, exact wide-number preservation and no HTTP fetch. White-box tests count actual converter entries: depth 8 takes 50 visits and 10 cached reference pairs; depth 40 takes 210 visits and 42 pairs. Removing memo reads gives 4,093 visits at depth 8 and fails the bound. This is a structural-work witness, not CPU or allocation profiling. Additional negative overlays bypassing reference validation and dropping retained inputs are rejected. The old-production overlay fails the source oracle, 12 reference-table cases, ownership and unknown-resource rejection. These overlays and all extracted sources, adapters and disposable logs were removed after analysis.

A focused caller regression, `TestValidateTypeChecks`, already failed on `faef815`: it expected scalar array input to reject. The actual pinned compiler wraps that input. Its expectation now checks the exact one-element array, with the same full schema/input independently covered by the 16-control source run. No production change was needed for that correction.

Go `1.27.3`, `GOMAXPROCS=4`, with `scripts/project-env.sh` sourced:

```sh
filter='^(TestValidationReference|TestUpstreamMainValidation|TestUpstreamValidation|TestTool130Argument|TestValidateTool|TestValidateTypeChecks$|TestAgentLoopHarness$)'
CGO_ENABLED=0 go test . ./tests -run "$filter" -count=10 -shuffle=42 -timeout=90s
CGO_ENABLED=1 go test . ./tests -run "$filter" -count=10 -shuffle=7 -timeout=90s
CGO_ENABLED=1 go test -race . ./tests -run "$filter" -count=5 -shuffle=42 -timeout=90s
```

Root/test-package times are 0.009/0.351 seconds, 0.006/0.362 seconds and 1.024/1.294 seconds respectively. Vet and staticcheck v0.8.2 pass for `.` and `./tests` in both CGO modes; module verification passes. Staticcheck initially flagged the capitalised diagnostic prefix; a single ST1005 directive documents the required upstream outcome protocol. No full-suite/full-race run or profiling.

## Large-enum checkpoint at c9f4c7b9

`tests/upstream_validation_enum_main_test.go` ports both assertions in `validation-probes.test.ts:718–725`: accept `member9999` from 10,000 string choices, and reject `x` with exactly the first 20 choices followed by `... and 9980 more]`. Unchanged `826a3b1` accepts the last member but emits a generic rejection. Five source-derived controls also fail: list sizes 0, 1, 20 and 21, plus HTML/line-separator/literal-escape text.

The diagnostic now serialises at most 20 entries using the existing JS-compatible `SafeJSONStringify`, then appends the exact omitted count. Validation/coercion and accepted values are unchanged. The original upstream body/helper, including both assertions, and all five controls pass actual pinned upstream code under Bun assertion/package-export adapters. This selected run does not execute Vitest. The old-production overlay and three mutations are rejected: a 19-entry limit, the total count instead of the omitted count, and Go-escaped line separators. Extracted sources, adapters, overlays and logs were removed after analysis.

Focused Go `1.27.3`/`GOMAXPROCS=4` checks use the preceding recursive-checkpoint filter plus `TestProviderErrorSerializationTextMain` and `TestErrorJSONSeparatorEscapeParityMain`. Ten normal repetitions per CGO mode (shuffle 42/7) pass: root/tests 0.011/0.408 seconds and 0.007/0.413 seconds. Five race repetitions (shuffle 42) pass in 1.032/1.612 seconds. Vet/staticcheck v0.8.2 for `.` and `./tests` pass both CGO modes; module verification and diff checks pass. No full-suite/full-race run or profiling. Non-string enum validation and exact diagnostics outside these covered string cases are open.

## Numeric-bound witnesses before cache identity

`tests/upstream_validation_numeric_main_test.go` ports the exact numeric `exclusiveMinimum` schema/inputs at source line 347 and literal rejection snapshots at 570–571. It also preserves the draft-04 boolean-exclusive schemas/results at 217–223 and 524–525: zero remains accepted because upstream ignores boolean exclusivity. The first assertion of the cache probe at 727–732, parsing `n: "4"` under minimum zero, passes unchanged native code; its mutation assertion is not yet ported.

Fresh-schema minimum-ten controls prevent a false cache pass caused by ignored bounds. Unchanged `4ac47ff` fails eight of the 14 JSON cases: accepted invalid bounds and rejected valid bounded `oneOf`. The fix applies `minimum`, `maximum` and numeric exclusive bounds during non-coercing membership checks, including untyped schemas, then reports the source comparison diagnostic. Conversion remains independent, so an invalid branch conversion can still satisfy another union branch.

Native supplements preserve `int64` and canonical integer `json.Number` values without rounding into acceptance or rewriting successful results. Rounded ties around 2^53 and ±2^63 receive exact comparisons; only larger canonical-integer ties need `math/big`. Fractional `json.Number` comparisons retain the existing float64/SDK domain. Non-finite values under numeric bounds reject. These native extensions do not establish arbitrary-precision fractional validation or full non-JSON diagnostic parity.

The original numeric-exclusive entry, its two literal snapshots/helper and all 14 JSON cases pass actual pinned upstream code under Bun/package-export adapters. Plain-JSON cache/fresh-object controls independently confirm that upstream keeps the first validator after mutation but rejects a fresh minimum-ten object. Bounded NaN/infinity controls also reject upstream. This selected run does not execute Vitest. Old production and four mutations are rejected: skipped bounds, rounded int64 ties, rounded larger integer ties and premature conversion gating. All extracted sources, adapters, overlays and disposable logs were removed after analysis.

Focused Go 1.27.3/GOMAXPROCS=4 checks use the preceding enum-checkpoint filter. Ten normal repetitions per CGO mode, shuffle 42/7, pass: root/tests 0.006/0.415 seconds and 0.006/0.421 seconds. Five race repetitions, shuffle 42, pass in 1.028/1.624 seconds. Vet/staticcheck v0.8.2 for `.` and `./tests` pass both CGO modes; module verification and diff checks pass. No full-suite/full-race run or profiling.

Cache identity remains open; the investigation below rules out a byte-buffer cache as a faithful representation of the full mutable-object contract. The bounds controls distinguish genuine cache behaviour from ignored constraints.

## Cache identity: checks and live schema objects

The exact upstream cache body/helper at 727–732 passes actual pinned validation/compiler sources with TypeBox 1.3.27 under Bun. Package-export and assertion adapters replace the runner only, not schema builders or validation. Eleven plain-JSON mutation/fresh-object sequences additionally establish:

| Mutation after an initial successful parse | Cached result | Fresh-object result |
| --- | --- | --- |
| Number minimum zero to ten | `n: 4` | Reject: `n: expected >= 10` |
| Add boolean property, input `flag: "true"` | `flag: true` | `flag: true` |
| Change number property type to string | Reject: `root: invalid` | `n: "4"` |
| In-place union child change described below | `n: 1` | `n: "1"` |
| Replace the same union child | `n: true` | `n: "1"` |

The union starts with `n: {anyOf: [{type: "number", minimum: 0}, {type: "boolean"}]}` and parses `n: "4"` once. Then either mutate the first child's type to string, or replace that child with `{type: "string", minimum: 0}`. Parse numeric `n: 1`. Independently deep-compared final JSON schema values are identical, but cached results differ as shown. The `minimum` matters: a bare-type check bypasses the compiler's node cache and does not expose this distinction.

Upstream `validator.ts` keeps a compiled check, but builds a separate coercer/compiler whose node checks are cached by object identity. Conversion still reads live schema members. Freezing a complete decoded schema would incorrectly miss new-property conversion. Keying by JSON bytes, content or backing buffer cannot distinguish in-place nested mutation from replacement when their JSON values are identical.

Native `Tool.Parameters` is `json.RawMessage`; its public data model loses this identity history. No approximate cache or API break was added, and the mutation assertion is not credited as ported. An additive object-backed schema boundary is the contract-preserving direction to investigate, retaining byte-schema compatibility and coherent validation/provider-wire authority. Its design must cover per-node identity, ownership, concurrent use, cache lifetime/GC and copied tools. This remains an explicit API decision, not an accepted divergence. Raw probe scripts are disposable; only these conclusions are retained.

## Selected format assertions at c9f4c7b9

`tests/upstream_validation_formats_main_test.go` reads `tests/testdata/upstream-main/validation-formats.json`. Eight literal inputs/snapshots from source 128–152/495–506 use the complete original schema: email, URI, UUID, IPv4 and the made-up-format control. The fixture's schema and literal inputs/expectations independently match actual TypeBox 1.3.27/source snapshots. All eight plus 31 separately labelled source-derived controls pass pinned upstream validation/compiler code under Bun runner/package-export adapters, not Vitest. The four remaining literal format cases—date-time (two), hostname and regex—are not credited.

Unchanged `5b2c091` fails 19 of these 39 native assertions: four literal invalid strings are accepted, 13 derived invalid strings are accepted, oneOf wrongly rejects a valid email and anyOf leaves a numeric string unconverted. Non-coercing format membership plus `expected format <name>` diagnostics fixes them through public validation, including local references. Unknown names remain annotations and formats apply only to strings. Known upstream formats outside this selected implementation remain gaps, not unknown-format exemptions.

The checked-in generator copies the four reviewed regex definitions from the exact pin; it does not substitute stricter mail/URL/IP libraries. Source-permissive bracketed IPv6 email and URI authority/reg-name cases stay accepted. ASCII-only comparison folding preserves JS non-Unicode `/i`: Kelvin sign and long s must not fold into ASCII. Successful values retain their original casing. Pattern constants compile once; Go's bounded regex engine does not perform network lookups. The original MIT notice is retained in `THIRD_PARTY_NOTICES.md`; no Go module dependency changes.

Portable regeneration uses the Makefile's existing project-owned cache/temp resolution:

```sh
GOTOOLCHAIN=go1.27.3 make generate-schema-formats UPSTREAM_REPO=/path/to/upstream-git-repository
```

Four parsed pattern values match pinned source definitions and remain stable on regeneration; a changed UUID definition is rejected by the semantic-data comparator. This is not an output-file hash gate. Old-production and skipped-format overlays each fail 19 assertions; Unicode-fold, missing-ASCII-fold and wrong-diagnostic mutations fail 3, 4 and 17 respectively. The initial overlay omitted logical PWD and was detected as inactive; corrected overlays cover logical and canonical paths.

Final focused filter is the recursive-checkpoint filter above. Go 1.27.3/GOMAXPROCS=4, ten normal repetitions per CGO mode, shuffle 42/7: root/tests 0.008/0.442 seconds and 0.015/0.446 seconds. Five race repetitions, shuffle 42: 1.038/1.644 seconds. Vet and staticcheck v0.8.2 for `.` and `./tests` pass both modes; module verification and diff checks pass. An initial invocation inherited Go 1.27.1; baseline and quoted verification were rerun with explicit 1.27.3. Raw source/adapters/overlays/logs and isolated dependency cache were disposed after analysis. No full-suite/full-race run or profiling.

## Temporal formats at c9f4c7b9

The temporal fixture adds the exact two date-time matrix assertions at 128–152/499–500 and lowercase/space-separator assertions at 392–394/587–588. Their actual TypeBox/plain-JSON schemas, inputs and snapshots independently match `tests/testdata/upstream-main/validation-temporal.json`. All four plus 52 labelled source-derived controls pass actual pinned validation/compiler code with TypeBox 1.3.27 under Bun adapters, not Vitest. The shared native fixture runner preserves the earlier eight literal/31 derived cases unchanged. Ten of the twelve original format-matrix assertions are now ported; hostname and regex remain open, as does the separate URL assertion.

Unchanged `0d68e82` fails 33 of the 56 new assertions. Registered `date`, `time` and `date-time` checks now follow the source calendar/time algorithms: exactly four year digits, Gregorian leap years including year zero, strict timezone and seconds, lowercase `t`/`z`, arbitrary fractional digits and offsets through 23:59. Second 60 must normalize to UTC minute 23:59, including day wrap; source does not consult an official leap-second calendar. Parsing checks string syntax without rewriting values or using the different `time.Parse` contract. The existing non-coercing format path covers unions, untyped schemas and local references. The retained TypeBox MIT notice now includes these algorithms.

A deterministic independent public-path sweep matches actual source and native acceptance for 9,224 cases: 3,960 date boundaries, 5,200 time/fraction/offset combinations and 64 combined date-times. Successful strings and caller inputs remain unchanged. This supplements the durable regression fixtures; it is not a complete format suite. Old-production overlay fails 33 assertions. Missing-timezone, reversed-offset, no-leap-second and wrong-century-rule mutants fail 2, 4, 8 and 1 assertions. An initial zone-only mutation retained the short-length rejection and was ineffective; the corrected mutation actually admits missing timezones before checking the gate.

Final Go 1.27.3/GOMAXPROCS=4 checks use the recursive-checkpoint filter above. Ten normal repetitions per CGO mode, shuffle 42/7: root/tests 0.006/0.581 seconds and 0.007/0.523 seconds. Five race repetitions, shuffle 42: 1.031/1.696 seconds. Vet/staticcheck v0.8.2 for `.` and `./tests` pass both modes; module verification and diff checks pass. No full-suite/full-race run or profiling. Extracted source, isolated TypeBox/cache, sweep program, overlays and logs were disposed after analysis; only fixtures and concise conclusions remain.

## Hostname assertions at c9f4c7b9

The exact hostname matrix input/snapshot at 128–152/502 and 23 labelled controls run through public validation in `TestUpstreamMainValidationHostname`. `TestUpstreamMainValidationHostnameDependency` preserves all 66 assertions in the original draft-2020-12 hostname fixture. The Tool API requires an object, so each dependency datum is wrapped in required property `h` with the original format-only schema; no string type is added and non-string data still pass unchanged. All 66 also pass actual upstream directly and through that wrapper. The literal schema/input/snapshot matches actual TypeBox 1.3.27; controls pass actual pinned validation/compiler sources under Bun, not Vitest.

Unchanged `20342b7` fails 50 assertions: 13 literal/derived and 37 dependency cases. The direct hostname/punycode port passes all 90 final assertions. It preserves source UTF-16 length limits (253 total/63 per label), ASCII hyphen/ACE reservation, Unicode payload casing, non-ASCII decoded-label requirement, context and bidi rules. It does not silently add NFC, strict joining-type or final-bidi checks absent upstream. Supplementary witnesses retain decomposed labels, permissive RTL endings and non-virama/non-ASCII ZWNJ; reject dotted-I casing errors; and distinguish UTF-16 from UTF-8 at both length boundaries. Decoding bounds insertion indices before arithmetic can overflow; canonical encoded length is checked without allocating a second label. Casers are per call, not shared mutable state. No DNS or network lookup occurs. Existing `x/text` is reused without dependency changes; TypeBox and fixture MIT notices are retained.

Both reference engines use Unicode 17. Every one of 1,114,112 code points matches actual source permitted-category, combining-mark, Greek/Hebrew/Japanese script, virama and bidi-class predicates. A separate seeded public-path comparison covers 9,320 encoded scalar/context/hostile payload cases and preserves successful strings and caller input. These diagnostics supplement, not replace, the retained literal/dependency fixtures. Other Unicode-engine versions were not tested. Old-production overlay fails 50 assertions; skipped Unicode validation, total-limit, simple-lowercase, skipped bidi-mixing and byte-length mutants fail 26, 1, 1, 1 and 2 respectively.

Final Go 1.27.3/GOMAXPROCS=4 focused filter is unchanged. Ten normal repetitions per CGO mode, shuffle 42/7: root/tests 0.006/0.466 seconds and 0.006/0.451 seconds. Five race repetitions, shuffle 42: 1.030/1.718 seconds. Vet/staticcheck v0.8.2 for `.` and `./tests` pass both modes; module verification and diff checks pass. Raw source, isolated dependency cache, Unicode flags/overlay, sweep program and logs were disposed after analysis. No full-suite/full-race run or profiling. Eleven of twelve original format-matrix assertions are now ported; regex, separate URL, IDN hostname and other formats remain open.

At this checkpoint, a separate required string/hostname probe found source numeric `12` becoming `"12"`, boolean `true` becoming `"true"` and null becoming `"null"`. Native numeric input rejected and null became an empty string. The following checkpoint addresses those string-conversion witnesses.

## Primitive-to-string assertions at c9f4c7b9

`TestUpstreamMainValidationStrings` retains both exact codec inputs/snapshots at `validation-probes.test.ts:462–463`, nine JSON-form successful `Value.Convert.String` assertions, its two BigInt successes adapted to Go `int64`, and three string-related assertions from `coerce.test.ts`. Scalar dependency inputs are wrapped in required property `v` for the Tool API. Successful objects compare structurally because Go maps have no property order. Actual TypeBox 1.3.27 and pinned validation/compiler/coercer code pass the original source bodies under Bun adapters. All 14 selected conversion-only string assertions pass upstream; native coverage credits the eleven successful observable conversions. The original object/array conversion-only results and undefined case have no equivalent public native result and remain unported.

The initial unchanged-`17c0e22` fixture/precision baseline fails 44 of 58 cases, including both cyclic assertions discussed below. Final string fixtures contain 58 passing cases: the retained assertions, 27 labelled JSON controls, four non-JSON float controls and eleven exact-native precision cases. Nine rejection fences, converted-array ownership and matching native-number branches also pass. The final old-production overlay fails 45 leaf tests, including ownership. Controls cover required/optional nulls, single-element type arrays, nullable and already-matching unions, enum members, arrays, local refs, sequential allOf, decimal thresholds, subnormals, signed zero and non-finite values under explicit string schemas.

Scalar string conversion now applies the source pass-2 null rule (`"null"`); type-array primitive conversion retains `""`. Finite doubles use the JSON writer's shortest decimal and ECMAScript notation thresholds. Signed zero becomes `"0"`; NaN and infinities retain JS string names. Supported native `int`/`int64` values use exact decimal digits. The `json.Number` extension canonicalises its exact decimal using the same notation thresholds without passing through float64; exponent arithmetic is checked and work/output size scales with the spelling, not the exponent. Invalid grammar and exponent-position overflow reject. This exact-decimal extension preserves quantities outside JS double precision/range. Object/Stringer inputs do not gain implicit stringification, and matching numeric branches retain native types/values.

Independent actual-source/native public-path probes match 41,898 IEEE bit-pattern and decimal-threshold cases, including non-finites and caller-bit preservation. Another 2,000 exact-native decimal cases preserve quantities against independent `big.Rat` parsing and leave inputs unchanged. Old production plus primitive-null, generic `g` formatting, signed-zero, rounded-int64, rounded-decimal and converted-matching-type mutations fail 45/9/5/1/2/6/3 leaf tests respectively.

The old AJV-era required-null string expectation was incompatible with the current pinned test. `TestUpstreamValidationCoercesSerializedPlainJSONSchemas` now preserves sixteen of its twenty current positive assertions, including scalar-to-array conversion; rejection inputs match the four current invalid cases. Actual source passes all twenty positives. Four further positives still fail native: boolean `"1"`/`"0"`, null `"null"`, and fractional integer `"42.1"`. They retain their original assertions in `validation-primitives-pending.json`. Both original cyclic snapshots at 458/460 also fail: even the already-string control rejects because named `$id`/`$ref: "Node"` resources are unresolved. They are retained separately in `validation-cyclic-pending.json`; string conversion did not fix named-resource resolution.

At `7f3275f`, all six explicit pending oracles were executed and failed; Go excludes `testdata` from `./...`. The following checkpoint promotes the four primitive cases. Both named-resource failures remain in the explicit pending package. Other primitive rules, non-JSON numeric unions, conversion-only ownership/identity, named resources and the broader schema matrix remain open.

Final Go 1.27.3/GOMAXPROCS=4 focused filter is unchanged. Ten normal repetitions per CGO mode, shuffle 42/7: root/tests 0.006/0.490 seconds and 0.011/0.473 seconds. Five race repetitions, shuffle 42: 1.045/1.797 seconds. Vet/staticcheck v0.8.2 for `.` and `./tests`, both modes, module verification and diff checks pass. No dependency change, full-suite/full-race run or profiling. Extracted sources, isolated dependency cache, sweep programs, overlays and logs were disposed after analysis; fixtures and concise conclusions remain.

## Primitive conversion assertions at c9f4c7b9

The four retained plain-schema assertions now pass: boolean `"1"`/`"0"` become true/false, null `"null"` becomes null, and integer `"42.1"` becomes 42. `TestUpstreamValidationCoercesSerializedPlainJSONSchemas` now covers all twenty positive assertions. The four original schemas, inputs and results moved from `validation-primitives-pending.json` into `validation-primitives.json` and `TestUpstreamMainValidationPrimitives`; the cyclic schemas and snapshots are unchanged.

Before the fix, the broader new native fixture and controls fail 48 leaf tests on `7f3275f`. All 96 original Boolean/Integer/Null/Number conversion bodies pass actual pinned `Coercer.convertOnly` with TypeBox 1.3.27 under Bun assertion adapters; the source's existing Symbol skip is unchanged. Public native coverage credits 65 successful JSON cases and five successful BigInt cases adapted explicitly to `int64` zero/one. Twenty unconvertible JSON results supply public rejection controls, without crediting an unavailable native conversion-only intermediate. Three undefined cases and three unconvertible BigInt cases have no equivalent native result and remain unported. Forty separately labelled source-derived cases cover numeric grammar, whitespace, radix, safe BigInt strings, scalar/type-array ordering and union selection. Native supplements protect exact decimals, matching wide numbers, negative zero and non-finite integer rejection. The upstream Vitest suite was not run.

`schema_validation_primitives.go` implements the scalar conversion pass separately from primitive type-array coercion. Scalar integers truncate towards zero; type-array integers do not truncate fractions. Boolean words and null tokens follow their source case rules. Number parsing accepts ECMAScript numeric grammar and whitespace, rejecting Go hex floats, underscores and non-finite string spellings. Radix parsing bounds temporary integer size. Already-matching native values stay untouched. The exact `json.Number` extension checks integer membership and truncates decimals without float64 rounding or exponent-sized expansion; tiny negative fractions retain negative zero. No public API or dependency changed.

Independent public-path comparison passes 12,432 actual-source/native scalar/type-array string cases, including signed zero, radix boundaries, hostile spellings and whitespace. Another 2,000 native decimal integer conversions match an independent `big.Rat` quotient oracle; callers remain unchanged. Old production and skip-scalar-pass, no-truncation, Go-parser, Go-whitespace, int-overflow, rounded-integer-check and scalarised-type-array overlays fail 48/35/5/4/2/2/2/5 leaf tests respectively. An initially ambiguous overlay replacement was rejected by its harness before execution, then corrected; it supplies no passing evidence.

Go 1.27.3/GOMAXPROCS=4 focused filter remains the recursive-checkpoint filter above. Ten normal repetitions per CGO mode, shuffle 42/7: root/tests 0.006/0.482 seconds and 0.017/0.480 seconds. Five race repetitions, shuffle 42: 1.028/1.843 seconds. Vet/staticcheck v0.8.2 for `.` and `./tests` pass both modes; module verification and diff checks pass. No full-suite/full-race run or profiling. Owned extracted sources, isolated dependencies, sweeps, overlays and disposable logs were removed after analysis.

`go test ./tests/testdata/upstream-main/pending -run '^TestCyclicNamedID$'` still fails both `cyclic-0` and `cyclic-1`. These failures concern named-resource indexing/resolution, including the already-string control. Next: inspect pinned resolver/compiler tests for nested `$id: "Node"` and bare `$ref: "Node"`, retaining the exact original schemas and snapshots. Remaining conversion domains and complete dependency-file parity are unverified.

## Open fidelity work

Reference resource IDs/anchors, broader recursive-schema/value domains, remaining formats (including regex/url/IDN hostname), schema-object cache/mutation identity, non-JSON types and the remaining probe matrix need source oracles. Full structured-clone equivalence, including returned aliases on unvisited paths and unsupported Go value types, is also open. Other keywords, including `multipleOf`, string length/pattern constraints, const/non-string enum validation, boolean subschemas outside covered object/reference cases, complete pattern/additional-property semantics and exact multi-error diagnostics still need coverage and implementation. ECMAScript-only regexes retain the earlier conservative handling. These focused slices do not establish full JSON Schema parity.

The new durable argument-type test requires a native typed-tool API mapping. The schema dependency has its own 210-path review queue; inspecting its source is not a passing dependency test suite. Review sources and translate assertions before changing more production behaviour.

The earlier generation persistence oracle still fails: four frames/four stored touches versus source `touches + 1`. Its generation/live/view sources and harness-view test are unchanged at this pin. See [generation oracle](generation-view-oracle.md).
