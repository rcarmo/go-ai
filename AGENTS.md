# Coding

* Follow YAGNI principles.

## Git workflow

- Never use `git rebase`. Always use `git merge` / `git pull --no-rebase`.
- Commit as `Rui Carmo <rui.carmo@gmail.com>` unless explicitly told otherwise.
- Configure both local and global Git identity before committing:
  - `git config user.name "Rui Carmo"`
  - `git config user.email "rui.carmo@gmail.com"`
  - `git config --global user.name "Rui Carmo"`
  - `git config --global user.email "rui.carmo@gmail.com"`

## Release parity documentation

- Maintain root `RELEASE.md` for every upstream `@earendil-works/pi-ai` release audit.
- Update `RELEASE.md` in the same release commit before declaring the audit complete.
- Each release update must record:
  - upstream release tag and SHA;
  - previous accepted baseline tag and SHA;
  - exact checkout paths or reproducible source locations;
  - exact changed-path matrix link;
  - text/image catalog comparator counts;
  - full-record text/image catalog deltas, including added, removed, and changed records;
  - exact npm package SHA-256 when npm artifacts are part of the audit;
  - every Go implementation, fix, adaptation, and N/A decision;
  - tests, deliberate fault-gate evidence, hosted CI evidence, and full validation-gate evidence.
- Do not report a release parity audit complete unless `RELEASE.md` is current for that release.

## Upstream parity workflow

- Audit only the latest official published upstream tag/artifact requested for the release and the exact prior accepted upstream tag. Never audit or diff against upstream `main` for release parity work unless explicitly instructed.
- Before implementation, record the current official release tag, tag SHA, prior accepted tag/SHA, npm artifact version, npm SHA-256, exact changed-path list, exact upstream test corpus, and full-record text/image catalog deltas.
- Derive changed-path and test-corpus crosswalks from the exact upstream tag range, then keep the release ledger and corpus manifest in sync with implementation decisions.
- Prefer production-path behavior tests over helper-only tests for provider/runtime changes. Cover wire serialization, raw HTTP/SSE behavior, parser behavior, replay semantics, and generated catalog metadata where applicable.
- Required local validation for upstream release parity includes:
  - focused tests for every changed behavior;
  - full-record clean text and image regeneration checks;
  - deliberate text and image fault gates that prove regeneration comparators fail on real drift;
  - `go test ./...`;
  - shuffled tests;
  - race tests with `CGO_ENABLED=1`;
  - `go vet ./...`;
  - `make staticcheck`;
  - `make check-logging`;
  - `make test-repro`.
- Do not hide or weaken tests. No hidden skips, broad TODO classifications, or unproven N/A claims are acceptable.
- Use a local-first workflow: finish implementation, docs, regeneration, deliberate fault gates, focused/full tests, race/static checks, and git hygiene locally before pushing.
- Hosted CI must run only once at the end of the local validation cycle. Do not use hosted CI as an iterative debugging loop; batch fixes locally into the final candidate push unless explicitly instructed otherwise.
- Resolve reviewer/auditor findings locally before the one final CI push.
- Final release parity state must be clean and synced with `origin/main`, Rui-authored, and committed without rebasing.
