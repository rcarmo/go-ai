# Pending upstream-main oracles

There are currently no Go fixtures in this directory. Previously pending tests were promoted without weakening their assertions:

* `harness_deferred_progress_test.go` is now `durable/harness_deferred_progress_main_test.go` at `2387014a690da8110d723067d99dfe4271ff0454`. It retains `memory > 12`, `stores < memory/1.5` and the first-memory-only raw-storage control. All three backends deliver20 partials/8 stored batches, versus the retained20/24 baseline.
* `view_generation_persistence_test.go` is now `durable/view_generation_persistence_main_test.go` at the same revision. The complete generation view, literal tuple, replay, frames/touches and idle assertions pass unchanged.
* The stored-observer oracle is `durable/session_deferred_observers_main_test.go`, promoted at `3bb8baef0cb9af092e88775bc70f77f45d433038`.

[Current generation integration receipt](../../../../docs/upstream-v1.5.0/generation-deferral.md) and [historical generation baseline](../../../../docs/upstream-main/generation-view-oracle.md) retain exact source/native pins, adapters and limitations. Existing scope does not establish full upstream parity.

Future known-failing fixtures belong here only with an explicit project-owned execution recipe, prototype exclusion, retained complete assertions and a concise baseline receipt. Never use a default skip or relaxed expected count to hide a failure.
