# Durable turn-limit boundaries and replay

Four more unchanged `harness-turn-limit.test.ts` originals passed after byte verification. Six existing Go groups passed twice before these tests were added. The public SQLite harness already matched the represented behaviour; this packet adds tests, not scheduler changes.

At the cap, explicit termination settles `done`. A handoff-only response writes its one reset before cutoff and settles `unanswered` with `turn_limit`. A manual `continue` uses the committed tool result and begins with a stored count of zero. A tool-owned child and a fork inherit the value; another conversation can choose its own limit.

The upstream restart fixture cuts a storage proxy. The Go fixture kills a child process at the held second tool, reopens SQLite, and checks the same persisted generation, replay-safe tool and count of two. It settles the original submission without another provider request. This proves the native cutoff witness, not identical backend internals.

The four new native witnesses passed three repeats. Two compiled controls caught changing termination precedence or removing inheritance. Nine focused groups then passed twice, with vet and Staticcheck. A wider check saw an intermittent failure in the existing unsafe-park recovery fixture; this packet does not change that path. Already-placed steer settlement, independent run budgets and other failed-run queue policies remain separate cases.
