<!-- RUI-PROFILE-LIFECYCLE-20261005 -->
## Current profiling and cleanup rule — supersedes older text below

Rui's explicit rule: **profile and tune during pre-release tests; remove profiling data immediately after analysis/use.** Ordinary development tests do not require profiling on every run. Targeted diagnostic profiling is optional when useful and follows the same disposal rule.

- During pre-release verification, capture CPU and heap/allocation behaviour, analyse hotspots and tune avoidable allocations/repeated work. Compare equivalent workloads without weakening correctness/security/numerical contracts. For Go, inspect CPU, alloc_space and alloc_objects; coverage alone is not profiling.
- Keep captures and matching artifacts only while the current analysis needs them. Once used, immediately delete raw profiles, traces, matching test binaries, temporary fixtures and disposable logs, including failed/probe artifacts after diagnosis. Retain only concise conclusions and important measurements/limitations. Do not keep indefinite raw archives or copy them into exports/reports/evidence to evade cleanup.
- Remove all completed disposable cache/build/test/run data promptly. Never delete files still in use: finish or safely pause the owning job and clean at a safe boundary. Preserve source, installed toolchains, durable datasets/checkpoints and intentional release assets. Minimise disk usage; no random exports or redundant snapshots.
- This rule overrides **every conflicting older paragraph in this file and linked local guidance**, including “profile every test”, “unprofiled tests prohibited”, “retain all raw evidence”, “never delete profiles” and cleanup exclusions based only on an evidence/profiles directory name. Update helper/CI cleanup behaviour accordingly; do not weaken pre-release analysis.
- Existing no-agent-contact and execution-pause rules remain unchanged. This policy grants no unsolicited coordination or automatic job restart.
<!-- /RUI-PROFILE-LIFECYCLE-20261005 -->



# Coding

* Follow YAGNI principles.

## Project-owned cache and temporary paths

- Canonical host root: `/workspace/tmp/go-ai/`. Reproducible caches use `cache/go-build`, `cache/go-mod`, `cache/go-path`, `cache/model-regeneration`, `cache/xdg`, `cache/npm`, `cache/bun`, and `cache/python`; ordinary build scratch uses `build/tmp`; isolated tests/regeneration use `runs/<purpose>/<run-id>`.
- `Makefile` exports `TMPDIR`, `TMP`, `TEMP`, `GOTMPDIR`, `GOCACHE`, `GOMODCACHE`, `GOPATH` and relevant Python/Bun/npm cache variables. Helpers source `scripts/project-env.sh`, which rejects paths outside the project root. For direct shell work, `source scripts/project-env.sh` before Go/Bun/Python commands. Never fall back to bare `/tmp` or home caches.
- The vendored `scripts/project-tmp.sh` snapshots inherited `TMPDIR` as `PROJECT_ORIGINAL_TMPDIR` and resolves once before exporting child paths. `PROJECT_TMP_BASE` selects `<base>/go-ai`; compatible `PROJECT_TMP_ROOT` must be absolute and end in `go-ai`. If both are set they must agree. Invalid, unusable or symlinked paths fail without fallback (this host's `/workspace` alias is allowed). CI chooses `$RUNNER_TEMP/go-ai`, original `$TMPDIR/go-ai`, then platform `/tmp/go-ai`, even when `/workspace` exists. Local hosts choose writable `/workspace/tmp/go-ai`, then platform `/tmp/go-ai`. Propagate the chosen root to children to prevent double nesting. CI requires no host-only helper or `/workspace/Makefile`.
- All fallback roots use the same `cache`, `build`, `tests`, `logs`, `runs` and `evidence` layout. `tests`/`logs` are disposable; test logs and profiles in `evidence` are transient and deleted immediately after analysis. Installed Go binaries/toolchains and pinned reference/data assets are preserved; this migration does not delete or relocate them.
- Capture CPU/heap profiles and matching binaries in transient `evidence/profiles` directories. After analysis, dispose them and full logs immediately; keep concise findings in `evidence/analysis`. The wrapper creates unique `runs/tests` scratch and removes it on exit. Test mutations must not target source/cache/conclusions.
- `make project-tmp-init` validates/creates paths without deletion. `clean` preserves conclusions and SBOM/release assets. Completed captures/runs/caches are disposable; never remove active work, source, fixtures/datasets, checkpoints or publication assets. No cross-project cleanup. Old completed go-ai profile/clone/log trees were disposed at Rui's request; old raw-evidence pointers are historical.

## Source tree layout

- The repository root is the public `goai` package. Keep production `.go` files and generated `models_generated.go` at the root unless a package-neutral move is proven necessary.
- Black-box tests that declare `package goai_test` live under `tests/` so the root public package stays focused on exported implementation files.
- White-box tests that need unexported access remain beside their package code, including any root tests that declare `package goai`.
- Generated files stay at their documented generator destinations; update generators and pinned inputs rather than hand-editing generated output.

## Change discipline

- Read relevant files before editing; never edit blind.
- Keep changes minimal and scoped. Preserve public compatibility unless a release audit explicitly requires a breaking change.
- Do not hand-edit generated artifacts. Generated Go source and image/text catalogs must come from exact pinned upstream inputs and checked-in generators.
- Preserve unrelated local work and do not weaken existing tests or gates.

## Official release discovery and bounds

- For upstream parity work, audit only the latest official published `@earendil-works/pi-ai` tag/artifact requested and the exact prior accepted upstream tag. Never audit, diff, or generate release parity from upstream `main` unless explicitly instructed.
- Before implementation, record:
  - upstream release tag and tag SHA;
  - prior accepted upstream tag and tag SHA;
  - npm package version, artifact provenance/path, and npm SHA-256;
  - exact changed-path `name-status` list and counts;
  - final upstream test corpus and changed-test markers;
  - full-record text and image catalog old→new counts plus added/removed/changed record counts.
- Keep exact source checkouts/artifacts reproducible and referenced from `RELEASE.md` and release ledgers.

## Coverage and evidence

- Maintain root `RELEASE.md` for every upstream `@earendil-works/pi-ai` release audit.
- Maintain the exact changed-path disposition matrix and per-file upstream test crosswalk alongside `RELEASE.md`.
- Every applicable upstream runtime/provider delta must be covered through production Go paths, not helper-only substitutes where transport differs.
- Prefer deterministic production-path tests for wire serialization, raw HTTP/SSE, parser behavior, replay semantics, errors, cancellation, usage accounting, and generated catalog metadata.
- Label live-credential/network-only remainders separately; give narrow, precise N/A rationales.
- No hidden skips, broad TODO classifications, test weakening, or unproven completion claims.
- `RELEASE.md` must record every Go implementation, fix, adaptation, N/A decision, local validation result, deliberate fault-gate result, SBOM/security evidence, hosted CI evidence, and final/rollback SHAs.

## Pre-release profiling and disposal

- During pre-release tests, capture CPU and heap/allocation behaviour, analyse hotspots and tune avoidable work using equivalent workloads. Ordinary development tests do not require profiling. Delete raw captures, matching binaries, traces and disposable logs immediately after analysis/use; retain only concise conclusions and measured results. Missing or incomplete capture is a limitation, not a profiling pass. Preserve active-job files until a safe boundary, source, durable datasets/checkpoints and intentional release artifacts.

- Prefer `make test`, `make test-race`, `make test-shuffle`, `make test-deterministic`, `make coverage`, `make bench` or `make fuzz`. All run through `scripts/test-profile.sh`, package by package, capturing and analysing cumulative CPU, `alloc_space` and `alloc_objects`, saving a compact summary and disposing raw output immediately.
- Focus a run with `PACKAGES=./durable TEST_FLAGS='-run TestName -count=3'`. `PROFILE_ROOT` is transient capture storage below the selected project root; disposable test roots use `runs/tests/<run-id>/`. The wrapper manages paths and rejects a successful run if required capture/analysis is missing. Exit traps dispose incomplete output too, after recording capture failures.
- Direct focused runs may use `go test ./package -cpuprofile=<run>/cpu.pprof -memprofile=<run>/heap.pprof -o <run>/test.bin`, followed by `go tool pprof -top -cum` for CPU and `-top -alloc_space` / `-top -alloc_objects` for heap. Capture separate profiles per package when testing several packages.
- Analyse profiles after failures too. Build failures and process crashes may prevent complete profiles; record a concise diagnostic and explicitly note unavailable data, then dispose failed output. Subprocess tests must profile their child work or explicitly use a profiled parent fixture; never silently drop child profiling.
- Inspect allocation and CPU call chains after each run. Record measured reductions against comparable workloads, or state why no safe change applies. Generated reports enforce capture and provide the starting analysis; they do not replace engineering judgement. Do not weaken assertions, increase acceptance deadlines or skip verification to improve performance numbers.

## Local gates and review

- Rui's current validation instruction (6 October 2026): use one modern Go toolchain only, currently Go 1.26.6, for remaining 1.0.4 work. Do not repeat minimum-version or dual-toolchain runs. Existing historical receipts and the declared module minimum keep their original scope; this instruction changes ongoing validation, not compatibility metadata.

- Required local validation for upstream release parity includes:
  - focused tests for every changed behavior;
  - full-record clean text and image regeneration checks;
  - independent deliberate text and image fault gates proving regeneration comparators fail on real drift;
  - `make test`;
  - `make test-shuffle`;
  - `make test-race`;
  - `go vet ./...`;
  - `make staticcheck`;
  - `make check-logging`;
  - `make test-repro`.
- Inspect diffs, generated drift, and `git diff --check` before committing.
- Resolve reviewer/auditor findings locally before the one final candidate push.

## Git and CI workflow

- Never use `git rebase`. Always use `git merge` / `git pull --no-rebase`.
- Commit as `Rui Carmo <rui.carmo@gmail.com>` unless explicitly told otherwise.
- Configure both local and global Git identity before committing:
  - `git config user.name "Rui Carmo"`
  - `git config user.email "rui.carmo@gmail.com"`
  - `git config --global user.name "Rui Carmo"`
  - `git config --global user.email "rui.carmo@gmail.com"`
- Use a local-first workflow: finish implementation, docs, regeneration, deliberate fault gates, focused/full tests, race/static checks, SBOM/security checks, and git hygiene locally before pushing.
- Hosted CI for developer/release candidate pushes must run only once at the end of the local validation cycle. Do not use hosted CI as an iterative debugging loop; batch fixes locally into the final candidate push unless explicitly instructed otherwise.
- Scheduled CI maintenance (`.github/workflows/ci.yml` weekly cron) is independent of developer candidate pushes and exists to refresh security/SBOM/license/fuzz evidence against live advisory databases.
- If final CI run metadata must be recorded after the final runtime candidate, use a docs-only `[skip ci]` commit or a proven paths-ignore mechanism, and record both the separately tested runtime SHA and final docs SHA.
- Final release parity state must be clean and synced with `origin/main`, Rui-authored, and non-rebased.

## Supply chain, SBOM, security, and licenses

- `make sbom` must generate a CycloneDX JSON SBOM plus SHA-256 checksum under the gitignored stable artifact directory `artifacts/` (`artifacts/sbom.cdx.json` and `artifacts/sbom.cdx.json.sha256`).
- SBOM generation uses the pinned `cyclonedx-gomod` version recorded in `Makefile`; do not use unpinned SBOM tooling.
- The SBOM must identify the root Go module/revision and resolved direct + transitive dependencies from `go.mod`/`go.sum`, avoid secrets and local absolute paths, and be normalized/reproducible or clearly artifact-only.
- `make sbom-check` must regenerate and validate the SBOM schema, required fields, checksum, root component, and non-empty dependency output; stale, malformed, path-leaking, or empty dependency SBOMs fail.
- `make vuln-check` must run a pinned `govulncheck` version. High/critical findings require documented owner, rationale, mitigation, and expiry before release completion.
- `make license-check` must run a pinned license scanner. Incompatible or unknown licenses require documented owner, rationale, mitigation, and expiry; never silently ignore them.
- Final CI must generate, validate, scan, and upload the SBOM and checksum with retention. Release evidence must record the SBOM tool/version, artifact names, digest, vulnerability scan disposition, and license review disposition.
- Do not commit volatile SBOM output or checksums.

## Native release tags and SBOM publication

- Publish accepted native versions under `vX.Y.Z`. The tag must be an annotated Git tag authored by `Rui Carmo <rui.carmo@gmail.com>` and must peel to the accepted runtime commit. Lightweight tags, bot-authored tags, docs/tooling heads, and overwritten or force-updated tags are invalid.
- Treat the peeled tag commit as the release target. GitHub's `target_commitish` field may report `main` for a release created from an existing annotated tag; it is not the provenance authority.
- Check that the native tag and release are absent immediately before creation. Create the annotated tag with create-only semantics, then verify its object type, tagger identity, and peeled commit before creating the release.
- Keep `upstream-vX.Y.Z` tags and releases as immutable historical aliases. Do not retarget them, replace their assets, or use their SBOM as the native release SBOM.
- Generate native SBOM assets from the exact accepted runtime. The CycloneDX root version, purl, and bom-ref must match `X.Y.Z`; the root must record the exact peeled commit as its VCS revision and have a root dependency edge.
- Publish only the canonical `sbom.cdx.json` and `sbom.cdx.json.sha256` assets unless a release contract explicitly adds another asset. Download the public assets after publication, validate the checksum, and compare them with the accepted local or exact-SHA hosted artifacts.
- Record and verify the release title `go-ai vX.Y.Z`, annotated tag object, peeled commit, asset digests, SBOM root/version/revision, dependency graph, and preserved upstream alias before reporting completion.
- Process historical versions oldest to newest. Stop on any tag, release, CI, security, licence, checksum, provenance, or publisher conflict.

## Lifecycle maintenance

- Keep lockfiles/manifests consistent with `go.mod` and `go.sum`; dependency changes must run `go mod tidy`, tests, SBOM, vulnerability, and license checks.
- Review dependencies and security posture at least weekly through the scheduled CI maintenance scan and immediately when urgent advisories are published or reported.
- Treat generated-data drift as a release blocker until explained by pinned upstream inputs and regeneration evidence.
- Review deprecations/removals for backward compatibility, migration notes, and tests before accepting upstream removals.
- Release/tag/changelog work must identify the accepted runtime SHA, final docs SHA if different, and rollback SHA.
- Preserve provenance/evidence pointers: upstream tag/artifact, npm SHA-256, local logs, CI run IDs, SBOM digest, scan dispositions, and fault-gate logs.
- After release, verify published artifacts/CI status and track any follow-up issues to closure.
- Lifecycle triggers include upstream release parity, dependency changes, release builds, scheduled weekly security scans, urgent security advisories, generated-data drift, and reviewer/auditor findings.

## Definition of Done

- Exact scope, changed-path matrix, and per-file test crosswalk are complete and current.
- Runtime/provider behavior has production-path evidence for every applicable delta.
- Text/image catalogs regenerate cleanly from pinned inputs and deliberate text + image fault gates fail as expected.
- Required Go gates pass with no hidden skips or weakened assertions.
- SBOM generation/check, vulnerability scan, and license review pass or have documented owner/rationale/mitigation/expiry.
- `RELEASE.md` records final release evidence, SBOM/security evidence, CI run, and rollback pointers.
- Exactly one final hosted CI run is green for the candidate, unless a docs-only `[skip ci]` evidence update is explicitly used.
- Git is clean and synced with `origin/main`; commits are Rui-authored and non-rebased.
