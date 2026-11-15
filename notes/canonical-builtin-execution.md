# Canonical builtin execution

Target: official pi-durable v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`.

GenerationTask2 and ToolTask3 are now real definition tokens held by every Registry. They execute canonical records through the generic TaskRuntime. Generation1 migration preserves its input/checkpoint. Tool1 migration adds the model discriminator; Tool2 preserves its input. Registry replacement is refused. Source: harness/registry.ts10–26, generation.ts162–175 and tool.ts136–158.

The canonical handlers read source input/checkpoint fields and committed records directly. They do not reconstruct private generation/tool checkpoints. Canonical nested calls create pi.tool3 with `kind:nested`, and use the source `taskId/result` family document. The private nested schema guard is excluded only when the document's owner has the canonical tool execution representation; existing private forgery controls retain their assertions. Canonical task commits no longer invoke the private generation-status projector.

## Verified packets

Tests are consolidated in `durable/builtin_tasks_test.go` and `registry_builtin_test.go`:

- Definition versions, initial checkpoints, generation identity migration and both original nested-tools1230–1250 tool-input migration cases.
- Imported Generation1/2 no-model records execute and persist the exact source terminal-state JSON.
- Actual canonical model answer and model→tool→model execution through Harness task creation, reservation and SQLite.
- Tool1/2/3 model calls migrate, execute once and append the model result entry.
- Unsafe stored intent does not execute; safe stored/current replay policy executes once.
- A throwing executor fails its task with `Tool thrower threw` and retains its model result reference.
- Canonical nested task kind/version/input/result, structured result and unavailable-tool settlement. Admission does not resolve the callee; abandonment follows the caller's replay policy, as tool.ts520–538 requires.
- Builtin definitions cannot be replaced through direct registration or extensions.

The former ordinary-suite builtin targets first failed on missing definitions and bounded import waits. Once execution worked, the no-model fixture's typed struct equality rejected identical JSON because of Go representation differences. It now compares the complete source JSON state without dropping fields or normalising aliases. Compile mistakes, that equality mistake, a missing reference package alias and a missing reference receipt callback receive no implementation-failure credit.

Further controls failed before their corrections: model executor throw0.114s, builtin replacement0.024s, nested private representation0.199s, and premature unavailable-callee rejection0.096s. Strict DTO checks caught typed diagnostic/usage values placed directly in JSON; these now cross the trusted encoder before persistence. The nested fixture declares the structured schema required by its result.

## Gates

The complete actual working tree passes26 tested packages: durable458.890s, tools21.525s, public consumers12.579s. No compiler overlay, excluded tests or weakened original assertions. An earlier complete run passed475.966s before the later nested/refusal corrections. Final ten shuffled canonical repetitions pass14.008s; forged-input controls pass three repetitions0.280s. Vet, Staticcheck v0.8.2, logging and parity register pass.

Six independently compiled semantic faults are detected: missing Tool1 discriminator migration, unsafe replay, wrong no-model state, invalid request phase, private nested definition reuse and missing builtin protection. Analysed scripts, logs and overlays were removed.

Selected original oracle:282 passing cases, zero skips (200 unchanged and82 independent probes). Original nested-tools plus the supplemental packet passes125 cases, zero skips. The latest reference probe checks actual Tool1/2/3 migration/execution and throwing-executor failure. These source executions do not establish native acceptance of all original assertions.

## Remaining work

**Full alignment is incomplete.** Normal Submit and legacy tool callers still create private builtin records; their production creation, cleanup and recovery paths must migrate to these definitions. Custom-task nested calls still use the private nested kind. The passing tests above cover canonical task creation and imported execution, not those producers.

All source phase names have executable handlers, but their full assertion packets are not accepted. In particular, complete provider-payload hooks, turn limits, terminate/afterRun control combinations, deferred cancellation/cutoff reporting, malformed-stream/error timing, progress/summary fidelity, full nested replay/lifetime cases and fault/orphan run cleanup require further source reconciliation. Canonical queue/boundary helpers need the complete original submission/inbox packet through the migrated producers. Shared record identity, original SQLite format/API and complete public type/lifecycle/readonly acceptance are open. The complete oracle/image equality and live-provider credentials are unaccepted. Pre-release CPU, alloc_space and alloc_objects profiling/tuning are incomplete.
