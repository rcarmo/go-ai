# Summary/reset and fork view oracles

Three tests from `packages/durable/test/harness-view.test.ts` at pinned `81f3cd412758ebdd92324c7d63b669a189408d0a` pass unchanged production in `durable/view_history_oracle_main_test.go`.

| Source | Native subtest | Preserved inputs and assertions |
| --- | --- | --- |
| 178–202 | `TestMainViewUpstreamHistoryOracles/summary-reset` | Separate a/b/c commits, watch, summary headed at b, d, public reset. Exactly three frames with kinds a/b/c/summary; a/b/c/summary/d; a/b/c/summary/d/pi.reset. Literal append tuple at index 3 containing the complete independently committed summary record; summary head equals b. Every frame replays; final replica equals a fresh committed six-entry view. |
| 204–217 | `/fork-markers` | Separate a/b commits, ownerless fork at b, watch, summary headed at b. Initial IDs equal a/b in order; first frame kinds equal a/b/summary; first frame equals a fresh committed three-entry fork view. |
| 219–232 | `/fork-isolation` | First entry, ownerless fork at first, watch, parent commit, child commit. Initial kinds equal first and ancestry names the parent conversation/cutoff. Exactly one delivered frame with kinds first/child. |

Existing head-marker coverage used app.note/app.summary, one initial multi-entry commit and an explicit callback wait between later writes. Existing fork-window coverage used different window/batch inputs. Those regressions remain intact; the new tests preserve the selected source scenarios independently.

## Native adaptations

`transcriptFixture` opens the native Harness and initialises its built-ins. Source chat/provider setup is replaced by native setup; the native pi.live shape differs from the source fixture. Entries require an empty `Value` object and minted IDs; expected complete summary records come from Session.Snapshot, independently of delivered frames. Source parent objects map to native `Conversation.Parent` and `ParentAt`. Native `Fork` defaults to ownerless here; public `Reset(bg, "")` represents reset without supplied text.

The recorder starts before the source follow-up commits. There are no inter-commit callback acknowledgements, sleeps, scheduler yields or added gates. It awaits the final entry only after all writes. The reset case reads the final committed view after all writes to identify the reset ID. Natural frame-array assertions pass, while general JavaScript Promise/microtask ordering and shared-reference equivalence remain open.

## Development checks

Go 1.27.3, GOMAXPROCS=4, project environment sourced:

- New history tests, 20 repetitions/shuffle 42: CGO 0 passes in 1.751s; CGO 1 passes in 3.946s.
- New history plus existing upstream advance/hydration and native head-marker/fork-history tests, race three repetitions/shuffle 7: passes in 6.446s.
- Durable vet and staticcheck v0.8.2: pass in both CGO modes. Module verification and diff checks pass.

These are test/documentation changes. No runtime, dependencies, persistence formats or release refs changed. The unrelated continuation prototype remains untracked and excluded; staticcheck temporarily moved it to owned scratch and restored it with a content comparison. No full-suite/full-race, mutation, performance or acceptance-profiling check was performed for this slice. Retirement/recreation and the remaining assertion-index rows stay open. Temporary upstream extracts and disposable scratch are removed after use.
