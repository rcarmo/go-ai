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
  - every Go implementation, fix, adaptation, and N/A decision;
  - tests and full validation-gate evidence.
- Do not report a release parity audit complete unless `RELEASE.md` is current for that release.
