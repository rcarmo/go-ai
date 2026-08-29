# Coding

* Follow YAGNI principles.

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

## Local gates and review

- Required local validation for upstream release parity includes:
  - focused tests for every changed behavior;
  - full-record clean text and image regeneration checks;
  - independent deliberate text and image fault gates proving regeneration comparators fail on real drift;
  - `go test ./...`;
  - `TMPDIR=/workspace/tmp go test -shuffle=on ./...`;
  - `TMPDIR=/workspace/tmp CGO_ENABLED=1 go test -race ./... -count=1`;
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
