# Rui's upstream-port preferences

Confirmed 2026-11-11 in the active alignment conversation:

- This is a port, not an alternate Go runtime contract. Full semantic alignment with pinned upstream is the target.
- There are no users. Backwards compatibility with the existing native Go implementation is not a constraint: replace incompatible APIs/behaviour and update callers/tests against upstream. Do not retain a second, weaker contract or move workarounds onto consumers.
- Be proactive within this authorised task. Continue across implementation, tests, crosswalks and acceptance checks without requiring another prompt after every bounded fix.
- Commit/push verified changes under the user's explicit existing authority. Do not call a slice full alignment; keep remaining gaps and acceptance failures explicit.
- Port pinned tests first, preserve their assertions, and independently probe source-derived contracts. Native green tests or reference-runner successes alone do not establish parity.
- Current target is official v1.5.0, f9a26365568127461c47a07a37dcb42c140a3ac5. Existing project scope covers pi-ai, pi-durable and pi-codemode; the active implementation work is durable.

These preferences do not authorise deleting data or published assets, touching unrelated work, unsolicited delegation, hosted release dispatch or publication. Pure-Go, validation, profiling/disposal and source-authority rules remain in AGENTS.md.

## Continuation pointers

- Current deviations/obligations: docs/upstream-v1.5.0/.
- Latest committed runtime checkpoint before this note: fdf1ce46c7c48efac236fa955218cb28a1245705.
- Writer is installed; the old memory entry saying otherwise was stale and has been corrected.
- Current cold-builtin hydration work is uncommitted: independent source probes15/145 unchanged tests pass; native cold tracker and LiveDoc v1/v2→v3 mount regressions failed before implementation and now pass, plus the view/watch/observer/builtin slice. Full/static verification and remaining builtin writer/migration acceptance are not yet complete.
- D04 continuation-turn bridge and D16 canonical public stored observation remain open. Do not replace them with private synchronous guards, sleeps or Gosched.
- Details: notes/readonly-document-watches.md; current user-editable Plan is the ongoing work checklist.
