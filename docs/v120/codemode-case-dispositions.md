# Codemode declaration and deterministic case dispositions

Pinned upstream `06a466935536f8c9bcacb2d5efc0fc021be1bdff`. Line numbers refer to the existing [declaration inventory](codemode-test-inventory.json), not executed upstream TypeScript tests. Native tests exercise the named contracts; unmatched API shapes are recorded explicitly.

## `declarations.test.ts` — 17 cases

All native paths below are under `durable/`.

| Upstream line / case | Native assertion | Disposition |
| --- | --- | --- |
| 7 primitives/literals/unions | `codemode_schema_crosswalk_120_test.go`, `codemode_schema_structural_120_test.go` | Exact representative renderings, including boolean/unknown/never. |
| 23 sorted object properties | `codemode_schema_crosswalk_120_test.go` | Exact quoted names, required/optional fields and additional-property renderings. |
| 39 property descriptions | `codemode_schema_crosswalk_120_test.go` | Exact nested comment placement. |
| 68 non-type constraint comments | `codemode_schema_annotation_crosswalk_120_test.go`, `codemode_schema_tags_120_test.go` | Exact tag grouping and order. |
| 102 optional non-null constraints | `codemode_schema_annotation_crosswalk_120_test.go`, `codemode_schema_constraints_120_test.go` | Nullable branch constraints and intersection with base bounds. |
| 132 draft-04 exclusive bounds | Same constraint/annotation fixtures | Boolean exclusive bounds produce pinned greater/less-than annotations. |
| 153 long tags/line terminators | `codemode_schema_tags_120_test.go`, annotation fixture | Long patterns omitted; descriptions retained; line separators escaped. |
| 179 drop tags before unknown fallback | `codemode_schema_fallback_120_test.go` | Exact fallback budget boundary; descriptions/literal whitespace retained. |
| 186 local references/recursion | `codemode_schema_reference_crosswalk_120_test.go`, `codemode_schema_refs_120_test.go` | Local pointers/escaped keys, recursive/remote/unresolved references and expansion bounds. |
| 209 arrays/tuples | `codemode_schema_tuple_120_test.go` | Exact array/tuple renderings. |
| 217 character budget | `codemode_schema_budget_120_test.go`, fallback fixture | Unknown fallback; native extra admission ceilings apply below. |
| 226 normalised signatures | `codemode_declarations_120_test.go` | Exact native async signature and normalised identifier. |
| 242 MCP-shaped output | `TestCodemode120MCPShapedOutputIsPlainDeclaration` | Added explicit plain-object output assertion; no MCP-specific output unwrapping. |
| 259 per-tool sample | `codemode_declarations_120_test.go` | Exact native sample wrapper. |
| 267 tools/globals | `codemode_declaration_blocks_120_test.go` | Exact block rendering and identifier normalisation. |
| 298 namespaced globals/explicit signatures | Same declaration-block fixture | Grouped namespace members and supplied signature. |
| 324 comment terminators | Same declaration-block fixture, comment fixtures | Escapes `*/` in tool/property comments. |

Native declaration input ceilings are 4096 nodes, 65536 bytes and depth32; local-reference expansion is bounded at2048 nodes. Default rendered character budget is16000. Inputs beyond these ceilings return `unknown`; they are not unbounded equivalents of upstream JS objects. No remote references are fetched. Tool registry schema validation remains a separate, stricter native subset. These bounds and async Go-facing sample formatting are explicit native API policies.

## `deterministic.test.ts` — 16 cases

| Upstream line / case | Native assertion | Disposition |
| --- | --- | --- |
| 108 repeated run reproduces | `durable/codemode_deterministic_crosswalk_120_test.go`, `internal/quickjs/deterministic_environment_120_test.go` | Pinned seed/start results and isolated repeated VM execution. |
| 116 UTC/no GC-dependent globals | Same fixtures | UTC date parsing/offset and absent WeakRef/FinalizationRegistry. |
| 127 delivery-sequence continuation order | `internal/quickjs/ordered_settlement_120_test.go`, durable official worker integration | Tool/global callbacks execute concurrently while settlement follows admitted sequence. Official worker completion order `[2,1]` is exercised separately. |
| 152 replay-stable fingerprint | `internal/quickjs/identity.go`, durable runtime-identity tests and worker integration | Native identity pins guest/prelude plus `host/62`; official worker fingerprint is independently checked. Identities deliberately differ. |
| 158 rejection error properties | QuickJS rejection-property, durable tool-error and official-worker property replay fixtures | Error message/properties persist through native and official-worker settlement/replay. |
| 190 catch errors without losing run | Same error-property/tool-error fixtures | Catchable tool rejection does not lose the VM result. |
| 210 monotonic clocks | `durable/codemode_quickjs_clock_120_test.go`, `codemode_delivery_clock_120_test.go` | Delivery advances Date/performance; backward delivery rejects without consuming call. |
| 225 validate clock before consuming | Delivery-clock fixture | Invalid timestamp can be corrected and retried. Native clock domain is nonnegative safe integers; JS nonfinite values are not representable as native int64 deliveries. |
| 237 validate unserialisable answer | Delivery-admission and delivery-clock fixtures | Cyclic/unserialisable answer rejected before consuming call. |
| 251 validate rejection properties | Delivery-clock fixture, worker malformed-batch cases | Success with rejection properties rejects; corrected result remains deliverable. |
| 267 during seq/issued metadata | Official-worker integration and `codemode_runtime_drain_120_test.go` | Native call-ID drains/issued counts preserve effect barriers; raw worker `during` metadata validates in the worker adapter. Go serial adapter has a different event API. |
| 276 custom script names/duplicate names | `internal/quickjs/script_names_120_test.go`, admission fixtures | Explicit custom JSName invokes the original host callback; default alias is unavailable; duplicate script names reject before host effects. Concurrent-driver ownership remains a separate contract. Exact error stack labels are not asserted. |
| 302 uncatchable abort | `codemode_parallel_abort_120_test.go`, QuickJS pending-callback cancellation tests | Parent cancellation aborts VM and joins nested callbacks; interrupted run does not become catchable JS success. |
| 317 finished lifecycle | `codemode_parallel_finish_120_test.go`, `internal/quickjs/ordered_settlement_120_test.go` | Terminal success/error discards later continuations and joins children. Native Wait/Close replaces JS `finished` promises. |
| 355 nonwaiting call/reused sequence | `codemode_delivery_admission_120_test.go`, remote/worker malformed fixtures | Unknown/stale/duplicate call delivery rejects; serial native call identity replaces worker sequence API. |
| 386 ordinary execute real environment | `internal/quickjs/ordinary_execution_120_test.go` | Ordinary execution uses real clock/randomness; explicit zero deterministic start/seed remain valid. Native WASI timezone remains UTC; upstream ambient host-timezone identity is not reproduced. |

## Verification and remaining work

Declaration/delivery/clock/concurrent-driver race selection passed three times in29.798s after adding the MCP-shaped declaration fixture. Earlier integrated runtime race covers production unchanged by this test-only addition. Custom script-name execution and collision admission now pass race count3 in7.581s. Ordinary ambient timezone remains a deterministic-family policy difference; error stack-label formatting is not asserted. Native budget, identity and event API policies are retained, not waived by this mapping. No upstream runner, live provider or Cloudflare deployment is counted as executed here.
