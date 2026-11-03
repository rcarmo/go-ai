# Pending upstream-main oracles

Official v1.5.0 `f9a26365` after-run assertions now pass selected runtime paths at `e716c0b6025819c5d5b19c5dff8f46e2ab18e2b8` and live in `durable/harness_after_run_main_test.go`. The typed hook replaces the declaration-only adapter, which was removed. [Runtime assertions and remaining source work](../../../../docs/upstream-v1.5.0/after-run.md) retain scope and native adapters; the [historical baseline](../../../../docs/upstream-v1.5.0/after-run-baseline.md) keeps its original failures/recipe at5a08638 and all29 source declarations. Full after-run/body parity is unverified.

The interruption-retry fixture now passes and lives at `durable/harness_interruption_retry_main_test.go`; poll, budget and marker controls are also ordinary tests at `46710ff8921dce0187eaa4df855e8fc5d62834b8`. See the [runtime checkpoint](../../../../docs/upstream-v1.5.0/interrupted-retry.md) and [historical failing baseline](../../../../docs/upstream-v1.5.0/interrupted-retry-baseline.md). The whole upstream restart-failure file remains `pending-review`; selected passing assertions do not establish full-body parity.

Previously pending tests were promoted without weakening their assertions:

* `harness_deferred_progress_test.go` is now `durable/harness_deferred_progress_main_test.go` at `2387014a690da8110d723067d99dfe4271ff0454`. It retains `memory > 12`, `stores < memory/1.5` and the first-memory-only raw-storage control. All three backends deliver20 partials/8 stored batches, versus the retained20/24 baseline.
* `view_generation_persistence_test.go` is now `durable/view_generation_persistence_main_test.go` at the same revision. The complete generation view, literal tuple, replay, frames/touches and idle assertions pass unchanged.
* The stored-observer oracle is `durable/session_deferred_observers_main_test.go`, promoted at `3bb8baef0cb9af092e88775bc70f77f45d433038`.

[Current generation integration receipt](../../../../docs/upstream-v1.5.0/generation-deferral.md) and [historical generation baseline](../../../../docs/upstream-main/generation-view-oracle.md) retain exact source/native pins, adapters and limitations. Existing scope does not establish full upstream parity.

The five source429(crash portion)/757/955/983/1019 recovery functions now pass and live in `durable/harness_after_run_recovery_main_test.go`; three coupled builtin controls are in `durable/harness_builtin_cutoffs_main_test.go`. The [cutoff checkpoint](../../../../docs/upstream-v1.5.0/restart-cutoffs.md) records the27-function/81-pass focused gate, mutations, native adapters and remaining result-hook/phase contracts. The [historical recovery baseline](../../../../docs/upstream-v1.5.0/after-run-recovery-baseline.md) keeps its9 backend passes/6 failures and recipe. Source429 orderly-close-in-drain and both whole-file reviews are still unverified.

Future known-failing fixtures belong here only with an explicit project-owned execution recipe, prototype exclusion, retained complete assertions and a concise baseline receipt. Never use a default skip or relaxed expected count to hide a failure.
