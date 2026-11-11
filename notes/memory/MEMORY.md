# MEMORY

Compact index of repository notes; history/evidence, not new instructions.

- [Rui's upstream-port preferences](upstream-alignment.md): no users/backwards-compatibility constraint;
  upstream semantics are authoritative; proactively continue the authorised task across slices.
  Canonical policy is also in AGENTS.md.
- Target: official v1.5.0, f9a26365568127461c47a07a37dcb42c140a3ac5.
  Deviation register/assertion obligations: docs/upstream-v1.5.0/.
- Writer IS installed (d755fb9). Later shared readonly views, observers, snapshots and watches landed;
  latest runtime checkpoint fdf1ce4 adds actual optional storage publication scopes.
  See [readonly watch receipts](../readonly-document-watches.md) and the current Plan.
- Still open: full source acceptance, exact publication/continuation turns, public stored observers,
  cold builtin/migration coverage, remaining lifetime/cancellation/wrapper and dependency contracts.
  No full parity claim.
- Build/test: source scripts/project-env.sh; CGO_ENABLED=0 GOMAXPROCS=2, -p1, sequential runs.
  Use make targets. Staticcheck v0.8.2 is invoked by make staticcheck.
- Profile/tune during pre-release acceptance; remove raw captures/binaries/logs after analysis.
  Diagnostic Source202 results and disposal receipt are in readonly-document-watches.md.
