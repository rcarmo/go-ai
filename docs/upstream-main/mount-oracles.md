# Conversation mount and pending-delivery oracles

Three pinned scenarios from `packages/durable/test/harness-view.test.ts` at `81f3cd412758ebdd92324c7d63b669a189408d0a` pass unchanged production in `durable/view_mount_oracle_main_test.go`.

| Source | Native subtest under TestMainViewUpstreamMountOracles | Preserved assertions |
| --- | --- | --- |
| 234–249 | `retire-recreate` | Watch, retire pi.live in one commit, recreate with tools:[] in the next. Exactly the delete tuple followed by the whole-document set tuple; first frame has no pi.live. No inter-commit callback barrier. |
| 251–272 | `undelivered-101` | Acquire without starting, append 101 separate kind:note entries, then start. One callback, first operation is not root replacement, operations replay from initial value to delivered value, and delivered value equals a committed newest-50 view. |
| 274–292 | `independent-remount` | Start two watches, append one, drain both, stop the peer, assert its single frame; append two and keep observing one/two through the surviving watch. Stop the last watch, acquire a new watch, compare its value with the last revision. |

Native setup initialises built-ins; entries require empty Value objects and minted IDs. Source tx.doc lazily creates the retired pi.live. Native builtin only looks up existing documents, so the port explicitly mints and creates an empty version-1 conversation document, then updates tools to an empty array in the same transaction. The first attempt used builtin for recreation and failed with builtin document missing on all three backends. This was a test-harness error; production was unchanged.

The source's rebuilt !== last assertion remains unported. Native detached Go structs do not represent shared JavaScript object identity. Value equality and successful reacquisition after both stops pass; internal shared-mount lifecycle equivalence and general Promise/microtask ordering remain open. The independent-watch scenario drains after the first commit as source does; retirement and recreation have no delivery barrier between commits.

## Development checks

Go 1.27.3, GOMAXPROCS=4, project environment sourced:

- New tests, ten repetitions/shuffle 42: CGO 1 passes in 9.691s; CGO 0 passes in 6.476s.
- Combined new mount, history, advance, hydration, atomic-publication and listener-isolation tests, race three repetitions/shuffle 7: passes in 19.250s.
- Durable vet and staticcheck v0.8.2 pass in both CGO modes. Module verification and diff checks pass.

Only tests and documentation changed. The unrelated continuation prototype remains untracked/excluded; it was temporarily moved to project scratch for static checks and restored with a content comparison. Temporary source extracts and scratch are disposed after use. No full-suite/full-race, mutation, performance or acceptance-profiling check was performed for this slice. Native 4,096-operation/DTO limits, broader source pending thresholds, reference semantics and the remaining assertion-index rows stay open.
