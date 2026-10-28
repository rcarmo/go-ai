# Native 1.4.0 checkpoint — 26 October 2026

This historical checkpoint recorded a Go 1.27.2 security blocker. Rui approved Go 1.27.3 on 28 October, and subsequent validation passed. Current results, exact 1.4.0 worker/provenance verification and final profiling decisions are in [acceptance.md](acceptance.md). Nothing has been pushed, dispatched, tagged or published.

## Scope

- Official target: `v1.4.0`, `4a080252f06caf63a525d7873c5d94d3c23887e0`.
- Prior accepted upstream: `v1.3.0`, `71023aec1546d6d677a65669c5665c0927ce0dd0`.
- Published pi-ai tarball SHA-256: `b40e2ba8bb6fc332908f8b96e837dc2d1fdfb45eb9cd382f5aed111462981e3c`.
- [Exact changed paths](changed-paths.json): 343 total, 178 relevant, 68 production and 109 test paths, using the recorded package/docs filter.
- [Published-provider catalogue delta](catalog-delta.json) records full-record additions, removals and metadata changes. That subset differs from the complete generated catalogue, which contains 1599 chat, 63 image and 51 classifier models.
- Rollback baseline: local `origin/main` at `16cc898`; all subsequent commits are local. Native 1.3.0 publication records remain unchanged.

## Runtime fixes verified

Local commits through `954c6b3` cover restart parking/progress, failed-run inbox policies, interruption/abort ordering, scoped observational telemetry, SQLite sleep recovery, paid-model telemetry inheritance, codemode checkpoint deltas and graceful-close requeue. Graceful close preserves checkpoints and restart counts without synthesising a crash outcome.

`954c6b3` adds adopted queue/batch input span links and compaction/deferred-cancellation telemetry and identity inheritance. Links and ancestry remain process-local. Tests cover dedup origins, backend panics, reentrant snapshot reads and both memory and journal storage. Explicit compaction telemetry takes precedence; pinned identity settings retain the normal generation precedence.

`e9de6b2` fixes ordering in the official-worker test transport: output/start/terminal batches enter synchronously; tool calls retain overlapped delivery. The unchanged 5000-output assertion caught a terminal batch overtaking earlier output. All official worker tests pass under race detection, count 3, with exact `@earendil-works/pi-codemode@1.3.0` installed. No default-suite skip substitutes for that explicit run.

The compaction alarm fixture accepts a zero-delay rearm only after its clock reaches the exact retry deadline. It still checks the initial two-second deadline, no premature provider retry and alarm-only continuation without Wait admission.

## Profiling changes

Go 1.27.2, Linux amd64, Intel i7-12700, identical benchmark fixtures, two-second runs, count 3. CPU cumulative, allocation space and allocation objects were inspected before and after each change. Raw captures and matching binaries were deleted after comparison.

| Workload | Before | After | Effect |
| --- | --- | --- | --- |
| `BenchmarkJSON140Checkpoint` | 60.620–63.369 µs; 10560 B; 405 allocations | 38.219–38.649 µs; 4096 B; 1 allocation | About 38% less time, 61% fewer bytes, 99.75% fewer allocations |
| `BenchmarkQuickJS140IsolatedEngineStartup` | 215.24–226.04 ms; 36.249 MB; 78745–78746 allocations | 6.367–6.430 ms; 1.851 MB; 21723 allocations | About 97.1% less time, 94.9% fewer bytes, 72.4% fewer allocations |

Plain JSON maps avoid reflection iterator slots. Named caller maps retain the generic path; nested values still pass marshal-authority, node, depth, string, member, cycle and byte-budget checks. Differential generic-path and cycle regressions pass.

QuickJS shares wazero's process-local immutable compilation cache for the fixed embedded module. Runtime ownership, guest memory, host state and cancellation stay separate. Closing one engine does not invalidate a sibling; guest globals and store data do not cross engines. Interpreter, isolation and cancellation tests pass. Remaining startup work includes WASM decoding and validation.

Full durable allocation-object totals fell from about 215.65 million to 129.94 million after the JSON change. Full-suite totals include scheduler/test overhead and are not a standalone timing comparison. The SSE-specific profiling review still needs completion; its short unit suite produced no CPU samples.

## Local gates

- Full profiled `make test`: passed after the worker-transport correction; durable 112.581 seconds before the compilation-cache change.
- Final profiled `make test-race` on `e9de6b2` sources: passed; durable 453.379 seconds, QuickJS 40.227 seconds, public-consumer tests 20.412 seconds.
- Final profiled `make test-shuffle`: passed after correcting the deadline fixture.
- `make test-repro`: full ordinary tests passed, then Staticcheck found an intentional nil-context test call and an unused Codex helper. The test now has a narrowly scoped lint annotation; the unused helper was removed. Staticcheck and focused Codex/telemetry tests then passed. The integrated target needs completion on the final candidate.
- `go vet ./...`, Staticcheck 0.8.2 and logging checks: passed.
- Exact v1.4.0 text/image/classifier regeneration: passed. Deliberate metadata-corruption gates passed after correcting their stale expected v1.3.0 diagnostic to v1.4.0. Failure and diff assertions remain intact.
- CycloneDX generation/schema/checksum checks and negative self-tests: passed with `cyclonedx-gomod@v1.12.0`, 24 dependency components. Preliminary SBOM root revision: `e9de6b2aebdea35e416c3baa1c37dfc95c2edbac`; digest `bcbd8ac86ab0aea477445d5e61d515a6599a460db580489c10bf6d23ac099537`. Regenerate for the final candidate; this is not a native-version release SBOM.
- Publisher/tag negative self-tests and vulnerability-policy self-tests: passed. No publication was attempted.
- Licence scan passed. Scanner warnings identify non-Go assembly in dependency packages that it cannot inspect further.

## Security blocker and next action

[`GO-2026-6809`](https://pkg.go.dev/vuln/GO-2026-6809), CVE-2026-111943, requires distrusting compromised intermediate certificates. Govulncheck 1.7.0 reports the reachable path from `oauth.EnableGitHubCopilotModelContext` through TLS to `crypto/x509.(*Certificate).Verify` on Go 1.27.2. The fix is Go 1.27.3.

`make vuln-check` fails with `undocumented reachable findings for go1.27.2: GO-2026-6809`. The policy permits no exceptions. Rui must approve changing the single validation toolchain from 1.27.2 to 1.27.3 before remediation and final checks. No exception, mitigation claim or toolchain change has been made.

After approval: update the toolchain pins, rerun security and affected final gates, finish the SSE profiling review, upstream per-file disposition/test crosswalk and npm provenance verification, then regenerate final SBOM/release evidence. Hosted CI, pushes, tags and publication require separate explicit authority.
