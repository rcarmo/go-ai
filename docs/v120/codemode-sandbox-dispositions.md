# Sandbox, source, discovery and model case dispositions

Pinned upstream `06a466935536f8c9bcacb2d5efc0fc021be1bdff`. This completes the family-level mapping of the149 inventoried single-line declarations alongside [declaration/deterministic](codemode-case-dispositions.md) and [durable output](codemode-output-dispositions.md) dispositions. The seven parameterised malformed-worker cases are separate. Native mapped fixtures do not imply execution of the upstream test runner.

## Sandbox — 52 declarations

| Contract family | Native evidence | Native policy / limit |
| --- | --- | --- |
| Return values, awaited promises, empty/undefined, text/image/console output, retained output on exceptions, string/BigInt errors | `internal/quickjs/output_crosswalk_120_test.go`, `portable_modes_120_test.go`, `engine_120_test.go` | JSON-safe result boundary; bounded bridge strings/output counts and aggregate bytes. |
| Tool calls, tool errors, catch-and-continue, custom names, arguments and concurrent calls | `internal/quickjs/tools_crosswalk_120_test.go`, `script_names_120_test.go`, `ordered_settlement_120_test.go`, durable parallel/mixed fixtures | Invalid/duplicate JS names reject before effects. Missing tool properties throw. Native callbacks use explicit host registrations. |
| Store/load, missing keys, deletion, snapshots, malformed/oversized values | `internal/quickjs/store_crosswalk_120_test.go`, durable store/delete/fault/recovery fixtures | Store mutation is detached, bounded and adopted only through successful durable settlement. |
| Custom globals, namespaces, spread arguments, global errors/collisions | `internal/quickjs/globals_crosswalk_120_test.go`, durable globals/boundary/revocation fixtures | Reserved roots, ambiguity and invalid names reject. Custom callbacks require captured versions and current authority. |
| Timeouts, abort before/during execution, unresolved promises, memory/stack/compute limits | QuickJS budget/admission/microtask/ordinary-execution/ordered-settlement suites; durable abort/deadline fixtures | Cancellation joins pending callbacks; native compute polls are separate from wall deadlines. Native stack256KiB, memory/bridge/setup ceilings remain enforced. |
| Isolation, frozen globals/intrinsics, independent execution and clean VM state | `internal/quickjs/isolation_crosswalk_120_test.go`, deterministic-environment, lifetime fixtures | Fresh WASM instances per execution. Host filesystem/network/timers are not exposed. |
| Worker process exit/malformed responses/transport cancellation | Durable remote/worker/process/cancel/portable suites | Explicit worker adapter has protocol/fingerprint admission. Test transport is not a production authenticated service; no live Cloudflare deployment receipt. |
| Source diagnostics and stacks | `internal/quickjs/output_crosswalk_120_test.go`, source fixtures | Runtime errors include `codemode.js`; stacks omit prelude internals. Exact engine-specific stack typography is not a functional parity gate. |

These groups cover the52 inventoried sandbox declarations; resource ceilings and platform worker entrypoints differ explicitly. Family mapping is less granular than a per-assertion differential report. Native execution plus official VM integration supplies practical contract evidence; no blanket claim of identical malformed-input acceptance or worker deployment is made.

## Source parser — 5 declarations

`durable/codemode_source_crosswalk_120_test.go` pins unchanged plain code, default options, first-line directive replacement without line-number drift, invalid source/directive/options and unsafe limits. `codemode_source_process_120_test.go` compares number parsing with published official code. Duplicate keys use the final value and JSON binary64 semantics are preserved; native safe-integer/native resource bounds remain admission policy. Source output/deadline/replay fixtures cover persisted options and Unicode output.

## Describe/prompt — 5 declarations

`durable/codemode_catalog_120_test.go`, `codemode_declaration_blocks_120_test.go`, `codemode_prompt_120_test.go`, `codemode_namespaces_120_test.go` and discovery-global fixtures exercise stable names, namespace grouping, prefix/deferred selection, inline prompt budgeting and invocation snapshots. Native prompt text uses the Go registration API and captured authority; exact prose/group ordering differs where it does not alter callable names or exposure. Namespace ambiguity is a rejection, not an authority expansion.

## Models — 7 declarations

`durable/codemode_model_catalog_120_test.go`, `codemode_models_120_test.go`, `codemode_model_failure_120_test.go`, `codemode_model_recovery_120_test.go`, `codemode_parallel_mixed_120_test.go`, paid classifier/image and auth tests cover sanitized credentials-free metadata, nested classifier/image calls, provider failures/headers, validation before effects, recovery, at most four paid calls in flight and post-completion cancellation. Images not explicitly emitted remain absent from model output. Paid globals are opt-in and can be revoked for new effects; replayed settled values do not repeat billing. Live paid-provider/auth acceptance is unrun.

## Portable — 7 declarations plus malformed parameter cases

The published official VM integration and native interpreter/compiler/process fixtures cover tools, globals, store, output batching (5000 items), output/service ceilings, cancellation, resource budgets and malformed remote requests. Separate malformed batch/delivery cases validate before effects or call consumption. Official VM was exercised with Node22.23.1, pi-codemode1.2.0 and quickjs-wasi3.6.2; upstream TypeScript test runner and deployed Cloudflare worker were not executed.

## Final runtime acceptance points

Exact wall-time text, UI labels and stack typography are diagnostic presentation differences; no new functional defect is inferred from them. Native ordinary timezone stays UTC, resource/schema/observer quotas remain intentional restrictions, and generated image encoding uses Go codecs. Large-root watch admission can fail even though persisted snapshots hydrate correctly. Those restrictions and external transport/platform coverage must remain visible in any parity claim. Security acceptance still fails13 reachable findings with the pinned Go1.27.1 toolchain; no exception or upgrade is authorised.
