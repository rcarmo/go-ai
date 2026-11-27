# Durable turn limits settle placed and queued submissions differently

The three remaining `harness-turn-limit.test.ts` originals passed after byte verification. Six unchanged native groups passed twice before the new tests. This completes execution of the 11 pinned cases across the [first round](durable-turn-limit.md), [boundary and replay checks](durable-turn-limit-boundaries.md) and this follow-up. Go's typed input and the native restart mechanism retain the differences recorded in those receipts.

The independent runs use separate budgets: the first answers below a cap of two and the second reaches the cap after two fresh tool rounds. The source's title mentions an answer at the limit; its body only witnesses an answer below the limit. On a real placed steer, the limiting round settles both inputs `unanswered` with `turn_limit`, records their IDs in order and makes no third request.

A follow-up submitted during the limiting round remains unplaced until the failed-run policy runs. `keep` leaves it queued, `continue` places and answers it, and `withdraw` settles it `unanswered` with `run_failed`. These variants check separate receipts and request counts on public SQLite paths. The pinned conditional assertions are indexed separately so each policy is tied to its own witness.

The three new native groups passed three repeats on unchanged production code. Compiled controls caught reversed failure attribution and withdrawal under `keep`. Nine focused groups passed twice, with vet and Staticcheck. No scheduler or storage changes were needed.
