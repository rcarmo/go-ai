# Release-preparation CI

Hosted validation runs only when an operator dispatches `.github/workflows/ci.yml` on a release-candidate ref with `release_preparation=true`. Ordinary pushes, pull requests and weekly schedules do not start validation. An unconfirmed dispatch skips the validation job.

Use the Actions UI, select **CI → Run workflow**, choose the candidate ref and confirm release preparation. Record the run's `head_sha` as the tested candidate. Dispatch does not grant tag, release or publication authority. The SBOM publisher remains a separate manual workflow. Artifact cleanup runs after CI completes; it does not start validation.

## Repeated failures diagnosed on 15 October 2026

The previous workflow ran on every main push, every main pull request and a weekly schedule. Incremental port pushes therefore repeatedly ran release-level gates against an incomplete candidate.

- [Run 38689447391](https://github.com/rcarmo/go-ai/actions/runs/38689447391), candidate `1547485a5842753e3b587698b35b9e0d64f9d07d`, failed `make check` at vulnerability-policy enforcement. SBOM validation passed with 24 components. The policy rejected 13 undocumented reachable findings for Go 1.27.1: GO-2026-6599, 6600, 6603, 6604, 6605, 6607, 6608, 6609, 6610, 6611, 6612, 6613 and 6617. [Run 38687740504](https://github.com/rcarmo/go-ai/actions/runs/38687740504) reached the same blocker. No exceptions have been approved.
- [Run 38688359904](https://github.com/rcarmo/go-ai/actions/runs/38688359904), candidate `4271d330`, failed deterministic tests three times in `TestQuickJS120GlobalLoopTimeoutClassification`. The 200ms async-global loop returned `sandbox: QuickJS bridge resource limit` instead of a timeout. The zero-latency loop can also exhaust the independent 4,096-call admission ceiling before the 200ms deadline, which correctly returns a sandbox resource-limit error. The wall-time test now paces callbacks by 1ms and asserts the call count remains below that ceiling. The separate `MaxCalls:32` test still verifies exact admission exhaustion. Production timeout and resource-limit behaviour are unchanged; a hosted rerun has not been dispatched.

Changing triggers removes repeated automatic runs. The competing-limit test has been corrected locally; the 13 security findings still block release validation. Release preparation still runs build, static/deterministic/race/coverage checks, security/SBOM/licence checks and fuzzing. Weekly and urgent-advisory review is local/operator work rather than an automatic hosted run.

`actionlint .github/workflows/ci.yml` passes after the trigger change. All workflow schemas pass `actionlint -shellcheck= .github/workflows/*.yml`; the unchanged publisher has two existing ShellCheck SC2129 style warnings under the full check. No hosted validation or publisher workflow was dispatched during this change. Failure logs were inspected directly without retaining downloaded archives.
