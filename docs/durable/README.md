# Native durable harness

`github.com/rcarmo/go-ai/durable` persists provider → owned host tool → provider answer runs in a native memory store or append journal. The useful M1 runtime is independently accepted and published in [native v1.0.1](https://github.com/rcarmo/go-ai/releases/tag/v1.0.1) and [upstream-v1.0.1](https://github.com/rcarmo/go-ai/releases/tag/upstream-v1.0.1), both targeting `33fcb88e6d9d5996279cd6f88687f988728aa2d8`. Full pi-durable parity is not complete; M2–M4 gaps below remain explicit.

## Run the local example

```sh
go run ./examples/durable
```

The example starts a local fake OpenAI HTTP/SSE server, registers a replay-safe sum tool and uses the exported `goai.Stream` provider path. It prints `The sum is 5.` without live credentials or an external server. Set `DURABLE_EXAMPLE_DIR` to retain its journal; the same request ID returns its stored answer on later runs.

## Public API

```go
store, err := durable.OpenJournal(dir, durable.JournalOptions{}) // Sync=true
registry := durable.NewRegistry()
err = registry.Register(durable.ToolRegistration{
    Definition: tool, Implementation: "host.sum", Version: 1,
    ReplaySafe: true, Execute: execute,
})
h, err := durable.Open(ctx, store, durable.Options{
    Models: resolveModel, Registry: registry, RequestOptions: resolveAuth,
})
root, err := h.Root(ctx, durable.AgentChange{Model: modelRef})
sub, err := root.Submit(ctx, durable.Input{Content: "hello", RequestID: "request-1"})
settled, err := sub.Wait(ctx)
err = h.Close(ctx)
```

Callers must check every error. `Open` adopts/reconciles committed state but starts no external effects. `Submit`, `Resume`, `Wait` and `WaitForIdle` may schedule pending work. Wait cancellation affects that waiter only. `Abort` commits cancellation marks before signalling and draining tools; child terminal receipts precede parent cleanup. `Close` fences new admissions and joins providers/tools and admitted commits, including noncooperative host code. A cancelled Close wait never releases storage ownership early or invents an aborted outcome.

Tool definitions are copied and the offered set is pinned with each request. Each tool has a distinct durable task ID and parent Owner; provider call IDs are transcript identifiers only. Arguments are bounded strict JSON, validated/repaired before intent, and the final execution arguments and implementation/version/replay policy commit before Execute. The assistant entry and successor model context keep the model's original call arguments; repaired arguments live separately in the owned tool checkpoint. Validators do not rerun on recovery.

`ToolAPI.Output` commits a bounded prefix (32KiB total), never raw volatile output. ToolResult content/details/validated usage and its application-document Commit callback adopt atomically with the tool result. Valid usage appears in both the result entry/checkpoint receipt and the same-commit `pi.usage.tools` aggregate, including error/aborted outcomes. Invalid usage (negative/overflow counters or negative/nonfinite costs) settles one redacted error with no application mutation or invalid spending; valid billed error usage is retained once. Application callbacks may mutate conversation-scoped non-`pi.*` documents only. Returned/escaped ToolAPI writes reject after invocation; Set/Update detach before returning. Credentials, headers, executable hooks and endpoint resolution stay process-local. The host must avoid putting secrets in application JSON, text or details.

## Validation and replay

The enforced schema subset supports explicit object/array/string/number/integer/boolean/null types, object properties/required/boolean additionalProperties, arrays with one item schema/minItems/maxItems, strings minLength/maxLength/Go RE2 pattern, numeric minimum/maximum and typed enums. Numbers are bounded to the exact `2^53−1` range; comparisons use exact decimal rationals with bounded spelling/exponents, so near-ceiling fractions never become integers through float rounding. Unsupported/malformed schemas reject at registration unless an explicit strict host Validator owns unsupported keywords; root tool arguments remain objects. This is not full JSON Schema/TypeBox validation.

Unsafe replay is the default. An interrupted started tool runs again only when stored and current ReplaySafe are true and name/implementation/version/schema identity still match. Stored unsafe/current safe cannot upgrade an old intent. Missing, changed or downgraded implementations produce conservative tool errors without effects. Replay-safe effects can occur twice after uncertain remote/outcome failure; host idempotency remains necessary. Terminal committed outcomes never rerun or double-count known usage.

Model requests pin sanitized model behaviour, settings and context cutoff; only matching local endpoint/authentication refresh. Terminal identity and spend use the pinned model. Payload rewrite hooks and unpinned behaviour options, including non-nil RetryConfig hidden by `json:"-"`, reject before intent/effects. Process-local read-only observers remain host code; no purity guarantee is supplied. Deferred requests are unsupported. Model requests can be billed again after an uncertain remote outcome; usage that never reached a durable receipt can be unknown.

## Storage and scope

The journal is native GODJNL1 framing, not upstream JSONL/SQLite interoperability. Default Sync=true; owner-only directory/file, strict JSON, durable ID reservations, coherent persisted resource caps, legal final torn-tail truncation and fail-closed complete corruption. Uncertain append/sync poisons reads/dispatch until close/reopen. Power-loss guarantees depend on filesystem/device semantics. In-process exclusivity is enforced; cross-process exclusivity and path replacement exclusion are host preconditions. No reclamation/encryption/OS lock or hard RSS bound is supplied. See [package format/limits](../../durable/doc.go).

The local native history/definition/delta checkpoint adds fork ancestry, half-open document incarnations, as-of/current copies, lazy migrations and decoded delta/checkpoint selection. These slices and their integrated durable regression are independently accepted; full-project successor gates and publication have not run. Generic task/extensions/subagent APIs, adaptive streaming and complete committed views, full steering/reset/post-tools boundaries, compaction, durable deferred/retry policies, legacy-agent history backfill, SQLite/reclamation and coding environment tools require further M2–M4 work. Built-in tools are host callbacks; the package does not silently run bash or access files.

[Contract/source crosswalk](contract-crosswalk.md) maps all60 fixed upstream source paths. [Test crosswalk](test-crosswalk.md) maps all42 fixed upstream suites with named native proofs and explicit remainders. Partial/unsupported suites are not counted as passing full upstream parity.

## Committed entries and observers (S2d, accepted subset)

S2d is independently focused- and integrated-accepted at candidate `5990d780dec1a03187f6a2d7badbedfb6cf096e1`, runtime `eb728455ab0444dc512990cabde8c48cef8ae428`. Focused normal2/race2 each passed 18 top-level tests plus 24 subtests, zero skips. Independent clean-clone validation passed 20 top-level tests (53 total passes), followed by a separate mixed-table publication/reopen probe on both backends. Integrated durable normal/race each passed 121 top-level tests plus 418 subtests, zero skips, at 11:20:28Z/11:22:07Z on 2026-10-04. Full 60-source/42-suite parity and full-project successor gates are unfinished.

The original candidate `9e78e3e9043ec4bb106eb38c6507b528f6e22cba` failed normal1 at 11:11:52Z: 15 top-level passes and two failures; race did not run. `HeadEditsToolPairsAndAncestor` and `PendingPreservedAndExactExcludedStops` rejected tool-call contributions on both backends because DTO `omitempty` lost empty `{}` arguments. Validated `EmptyArguments` witnesses now restore fresh empty maps before projection. Duplicate, out-of-range, wrong-type, conflicting and unmarked nil arguments still reject. Original logs and corrected focused/integrated receipts are preserved in `/workspace/tmp/go-ai-durable-s2d/`. The integrated sequential-admission maximum actual `Wait` was 2711.843ms, below its unchanged 3s limit; total test elapsed time includes work outside `Wait`.

Typed entry tokens narrow by kind. `Tx.AppendTypedEntry` copies optional strict data and curated model contributions; `Session.TypedEntry` and `ContextView` read committed ancestry with head/cutoff/edit reduction. Safe system sections and tool additions/removals are persisted through bounded receipt DTOs. Pending assistants remain in context; aborted/error/deferred contributions are excluded, matched tool results follow calls and missing results receive an explicit error receipt. Opaque signatures/control/deferred/raw diagnostic fields reject before staging; complete provider Message fidelity is unfinished.

`Session.WatchDefinition` acquires a detached baseline and registers on the same Session line. `Start` installs one asynchronous serialized listener; value/operations cannot mutate storage or other observers. Pending delivery is limited to 100 frames and a per-observer byte quota; overflow delivers a whole-value replacement. `SubscribeCommits` similarly returns a baseline plus complete adopted table/document batches, with an explicit snapshot after overflow. `DocumentState` is a disposable committed incarnation read handle.

Observer capacity is `min(MaxPage,64)` shared watches/subscriptions; each receives `MaxRetainedBytes/capacity` encoded pending bytes. Oversized baselines reject, and an oversized later reset ends the affected observer with `budget_exceeded`. Stop/cancel/Session close discard future delivery without waiting for a host callback already in flight. Retirement ends the old incarnation; recreation needs a new acquisition. Migration-only bases do not notify an observer already hydrated to that shape. Adaptive harness view/tree/event integration and generic task invocation observers require later work; the accepted slice covers the native committed observer APIs.

## Validation history and accepted publication

The original M1c candidate passed focused normal/race/shuffle controls and the independent public provider → owned-tool → answer probe. On2026-10-04, working-tree and genuine clean Git-clone runs each passed full tests, race, shuffle, vet, `make check`, `make test-repro`, SBOM/security/licence/self-tests, all-three-catalog regeneration, deliberate metadata corruption, twelve hydration faults and the public example:22 captured exit-zero gates. No hidden skips. Execution used nice10, GOMAXPROCS2, private Go cache and package/test parallelism2 during the granted serial window, released at00:59:08UTC.

Source/test validation tree `2c67427aead836afb7855c39ea16681b3a4f7222` was cloned from the local repository and matched the frozen private-index tree. Temporary validation commit `496796116d7c6ca592941057980a9827ca6e606d` exists only in the external clone; project HEAD stayed at M1b checkpoint `8d20e868e3931edb69dc5ee2085bdf8297f8323a`. Final receipt edits change documentation only and receive static checks. The working artifact identifies checkpoint8d20; the clone artifact identifies its synthetic validation snapshot. Neither is a published M1c runtime provenance receipt.

Evidence: `/workspace/tmp/go-ai-durable-m1c/full-validation/`, `clone-validation/`, `structural-review/` and final `frozen/` receipt. Earlier pre-gate snapshots and the failed fixture timeout are preserved. The partial fixture waits for durable append acknowledgement and always releases its provider before cleanup. Tool usage/repaired-argument structural corrections are covered by result-entry/checkpoint/aggregate and original-call/recovery controls.

Original committed candidate `a9d34b4a18f3a5641a75fb279c5cc09299ae3cee` failed the race step of normal run `37167059041`; fuzz passed, no artifact was uploaded and downstream steps skipped. Its normal history and evidence are preserved without retry. Go 1.24.13 diagnosis measured full-history strict-copy cost near the unchanged three-second sequential-admission budget; a private candidate copy-on-write repair preserved all public deep-detachment, strict validation, limits and atomic settlement. Post-repair race count3 reached maximum Wait1577.928ms versus prior2789.523ms, retaining exact20 settlements/calls and the same3s deadline.

The repair passed26 directly recorded working-tree/clean-Git-clone gate commands with actual Go 1.24.13 runtime tests: ownership/recovery controls, full normal/race/shuffle/vet/check/repro/security/licence/SBOM self-tests, all-three-catalog regeneration/faults and public example. Existing static/security toolchains remained pinned. Source/tests stayed frozen, the clone was genuinely clean, no failure was waived, and the serial gate window was released. Logs and exact exit TSVs: `/workspace/tmp/go-ai-durable-repair/`.

Accepted normal successor `33fcb88e6d9d5996279cd6f88687f988728aa2d8`, parent a9d, passed exact-SHA postcommit gates and sole hosted [run37168926337](https://github.com/rcarmo/go-ai/actions/runs/37168926337), attempt1, both jobs/all steps green with no skips. Artifact11289848481 ZIP/API digest `b76967f154b1a93fae8adafe21ad6e9c8d80860193a43f7ead4de2390babe579`; inner SBOM `687ae1e8e699afb5c7ed83ac736f505e9b884bc6ad03e7c3c3d420c2ec9999d1`, root short33f/full VCS33f,18/19/4 validated. Useful M1 runtime and both required same-v1.0.1 replacements are independently accepted.

Native current Rui-annotated object `913bdb5fa01ccc45d25ca9d231a435f093bd454a` peels to33f; publisher37169394645 updated existing release402718119 and canonical assets608932438/434. Native SBOM `933193f5fbfa7191f8c6f21e528fba102282bd4879b57a59fbc0576cdbc62442` identifies version1.0.1/full VCS33f and matches hosted after expected native root normalization. Lightweight alias also targets33f; publisher37169585232 updated existing release402719070/assets608937810/812; alias SBOM equals accepted hosted fields. Public bytes/API digests/sidecars validated. Initial55b/aa21 runtime/tag/32asset snapshots and all57 older refs/14 older releases/assets were preserved; total inventory59/16. Informational release target_commitish still reports55b; verified refs and SBOM VCS establish current runtime. See [root release receipt](../../RELEASE.md) for full publisher/job/asset/checksum identities and rollback evidence.

The final two-path documentation closure is a normal Rui `[skip ci]` child of33f; its exact SHA/guarded push/zero-Actions checks are recorded in `/workspace/tmp/go-ai-durable-final-docs/`. Static documentation checks only; no runtime/SBOM regeneration or docs-HEAD release tag. M2–M4 full-parity obligations require separate bounded implementation authority.
