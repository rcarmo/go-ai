# Native durable harness

`github.com/rcarmo/go-ai/durable` persists provider → owned host tool → provider answer runs in a native memory store or append journal. The local M1c candidate adds the tool loop to accepted M1a storage and M1b no-tool generation. It has not been independently accepted or published yet.

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

M1 supports current-only document bases, direct conversation history, follow-up/write dedup and queued withdrawal. Forks, historical document/delta/copy semantics, generic task/extensions/subagent APIs, watches/adaptive streaming, full steering/reset/post-tools boundaries, compaction, durable deferred/retry policies, SQLite/reclamation and coding environment tools require M2–M4. Built-in tools are host callbacks; the package does not silently run bash or access files.

[Contract/source crosswalk](contract-crosswalk.md) maps all60 fixed upstream source paths. [Test crosswalk](test-crosswalk.md) maps all42 fixed upstream suites with named native proofs and explicit remainders. Partial/unsupported suites are not counted as passing full upstream parity.

## Local M1c validation

The corrected candidate passed focused normal/race/shuffle controls and the independent public provider → owned-tool → answer probe. On2026-10-04, working-tree and genuine clean Git-clone runs each passed full tests, race, shuffle, vet, `make check`, `make test-repro`, SBOM/security/licence/self-tests, all-three-catalog regeneration, deliberate metadata corruption, twelve hydration faults and the public example:22 captured exit-zero gates. No hidden skips. Execution used nice10, GOMAXPROCS2, private Go cache and package/test parallelism2 during the granted serial window, released at00:59:08UTC.

Source/test validation tree `2c67427aead836afb7855c39ea16681b3a4f7222` was cloned from the local repository and matched the frozen private-index tree. Temporary validation commit `496796116d7c6ca592941057980a9827ca6e606d` exists only in the external clone; project HEAD stayed at M1b checkpoint `8d20e868e3931edb69dc5ee2085bdf8297f8323a`. Final receipt edits change documentation only and receive static checks. The working artifact identifies checkpoint8d20; the clone artifact identifies its synthetic validation snapshot. Neither is a published M1c runtime provenance receipt.

Evidence: `/workspace/tmp/go-ai-durable-m1c/full-validation/`, `clone-validation/`, `structural-review/` and final `frozen/` receipt. Earlier pre-gate snapshots and the failed fixture timeout are preserved. The partial fixture now waits for durable append acknowledgement and always releases its provider before cleanup. Tool usage/repaired-argument structural corrections are covered by result-entry/checkpoint/aggregate and original-call/recovery controls. Independent final acceptance, commit/push/hosted CI and coordinated same-version release replacement have not occurred.
