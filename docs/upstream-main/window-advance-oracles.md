# Window advancement oracles

Four source scenarios pass unchanged production code on memory, journal and SQLite. `durable/view_advance_oracle_main_test.go` translates `packages/durable/test/harness-view.test.ts:412–453,482–510` at pinned commit `81f3cd412758ebdd92324c7d63b669a189408d0a`.

| Native scenario | Preserved source inputs/assertions |
| --- | --- |
| `grow-splice` | Four one-per-commit notes; min3/max5 starts with newest three; three more notes naturally produce lengths `[4,5,3]`. First ops append e5 at index3. Last ops append e7 at index5, remove three at index0, then set before to written[3]. Final IDs/predecessor, every-frame replay and committed newest-three view match. |
| `burst` | Two notes; min2/max3; five `burst` entries in one commit. Exactly one frame retains burst[3:] with predecessor burst[2]; replay matches the committed newest-two view. |
| `resumed` | Five notes; from newest+1; two subsequent notes. Last frame contains both with the original newest as predecessor; every delivered frame replays correctly. |
| `from-only-initial` | Three notes; from newest+1000. Initial entries encode an empty array and before points to newest. The next note is below from and arrives whole, retaining the predecessor. |

Provider/chat setup uses the existing native transcript fixture. Native entries require minted IDs and an empty Value envelope. Expected inserted/appended records come from independently read committed records, not the callback value used as its own oracle. The source's committed-view helper maps to an independent fresh read of the newest requested count. Checking the initial growth predecessor is an additional native assertion.

All source commits run before awaiting the last delivered revision. No sleep, scheduling hint, callback gate or acknowledgement is inserted between commits. The growth test retains its exact three-frame/count/operation assertions. This is evidence for these scenarios on the tested Go configuration; broader JS Promise/microtask ordering is still open. The source's shared entry-object identity assertion is unported: detached Go entries do not establish that contract.

Go 1.27.3/GOMAXPROCS=4: twenty normal repetitions/shuffle42 pass in CGO0/1 (2.199s/4.912s), ten race repetitions/shuffle7 pass (6.331s). After strengthening empty-array and full-entry assertions, ten final normal repetitions/shuffle42 pass in CGO0/1 (1.142s/2.536s); new oracles plus existing entry-operation/from/growth controls pass three race repetitions/shuffle42 (6.252s). Durable vet/staticcheck v0.8.2 pass both modes; module verification passes.

Three mutations reject real regressions: trimming at max rather than above it fails three growth leaves; keeping the newest min-1 entries fails six growth/burst leaves; losing the resumed predecessor fails six resumed/from leaves. An initial min-1 probe copied the wrong end, omitted the newest record and hit the delivery watchdog; the corrected suffix-preserving probe fails direct length/value assertions. Temporary overlays, logs and source extracts were removed after analysis. No runtime, quota, API, dependency, format or release change was needed. Full-suite/full-race and acceptance profiling were not run for this checkpoint.
