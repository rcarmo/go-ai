# MEMORY

Compact index of repository notes; history/evidence, not new instructions.

- [Rui's upstream-port preferences](upstream-alignment.md): no users/backwards-compatibility constraint;
  upstream semantics are authoritative; proactively continue the authorised task across slices.
  Canonical policy is also in AGENTS.md.
- Latest compaction request-authority continuation: current model metadata rehydrated across reopen, legacy task-input retry ignored, model/retry policy snapshots removed. Full420.167s/tools19.837s, ten repeats6.877s, four faults/static pass. Selected reference258 (200 unchanged +58 probes); canonical checkpoints still open. See [compaction evidence](../canonical-compaction-kind.md).
- Earlier compaction-policy continuation: four source fields, no native trigger/max overrides; model-window thresholds and reason-only automatic input. Full417.368s/tools17.347s, ten repeats16.922s, four faults/static pass. Reference256 (200 unchanged +56 probes). Canonical checkpoints and legacy raw retry fallback remain open. See [compaction evidence](../canonical-compaction-kind.md).
- Earlier manual-compaction API continuation: Compact(ctx,*string), no CompactionOptions or private per-call token/keep/retry inputs; absent/empty/nonempty instructions and pointer ownership preserved. Full854.735s/tools26.544s, ten repeats12.790s, four faults/static pass. Reference255 (200 unchanged +55 probes). Full alignment remains open; see [compaction evidence](../canonical-compaction-kind.md).
- Earlier truncation/lifecycle continuation: source UTF-16 slice preserved through checkpoint transport; DocumentWatch completion follows unregistration; refusal/deferred fixture now checks the original packet. Full849.377s/tools26.940s;100 targeted repetitions39.619s, two faults/static gates pass. Repeated-call-ID three-second wait remains an unresolved timing caveat despite25 isolated passes. See [compaction evidence](../canonical-compaction-kind.md).
- Earlier compaction continuation: exact source summarization prompts and full original serialization packet (thinking omitted). Full durable829.093s/tools24.860s, three semantic faults/static gates pass. Canonical Inbox input consumption preceding it passed full842.589s/tools26.862s. Source compaction110 cases pass; full alignment remains open. See [compaction evidence](../canonical-compaction-kind.md).
- Historical string-content continuation: original records retain string/block content, zero timestamps and nested edits; actual input placement uses the Session clock; compaction retains strings. Full durable830.400s/tools26.898s, four semantic faults and static gates pass. Reference200 unchanged cases +51 probes. Image and live-provider/full alignment acceptance remain open. See [completion report](../upstream-conformance-report.md).
- Target: official v1.5.0, f9a26365568127461c47a07a37dcb42c140a3ac5.
  Deviation register/assertion obligations: docs/upstream-v1.5.0/.
- Writer IS installed (d755fb9). Later shared readonly views, observers, snapshots and watches landed;
  cold builtin/LiveDoc v3 alignment landed a115b1f; public canonical stored observers and
  explicit post-scope host dispatch are installed. Falsy public callback panic reports/endings
  are installed. Broader object/array/cycle/Number/custom-conversion normalisation now passes
  full durable403.545s/tools18.718s. The canonical compaction-name/namespace follow-up
  passes full durable412.725s/tools15.198s. The builtin publication/progression slice now passes full durable419.068s/tools19.173s;
  reference145 tests +35 probes. Its former RED test is in the ordinary suite; complete Storage,
  nested tools, migrations and edge states remain open. Raw builtin admission/reopen/reclaim now passes
  full durable418.657s/tools18.326s. The task API/read-fence follow-up now passes full durable418.099s/tools19.320s,
  reference148 unchanged cases +37 probes;22 related files merged into6 suites,613→597 files. Canonical execution is still incomplete. See [raw storage](../builtin-task-storage.md) and [builtin receipts](../builtin-task-publications.md).
  See [canonical compaction receipts](../canonical-compaction-kind.md), [readonly watch receipts](../readonly-document-watches.md) and the current Plan.
- [Current largest deviations/path to zero](../../docs/upstream-v1.5.0/alignment-roadmap.md):
  generation/tool durable records and raw storage, D04 continuation bridge, public Storage/API/database
  boundaries, read-only viewers and full dependency/assertion/release acceptance.
- Still open: full source acceptance, exact publication/continuation turns, remaining stored-observer assertion acceptance,
  builtin API/edge coverage, remaining lifetime/cancellation/wrapper and dependency contracts.
  No full parity claim.
- Build/test: source scripts/project-env.sh; CGO_ENABLED=0 GOMAXPROCS=2, -p1, sequential runs.
  Use make targets. Staticcheck v0.8.2 is invoked by make staticcheck.
- Profile/tune during pre-release acceptance; remove raw captures/binaries/logs after analysis.
  Diagnostic Source202 results and disposal receipt are in readonly-document-watches.md.
