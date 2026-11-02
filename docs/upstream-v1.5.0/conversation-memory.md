## Conversation views during deferred commits

Two new source-derived test functions pass unchanged native runtime `148c77b08c1d84a9cfca278380b3b872544a7ffe` at documentation head/base `180cab3550a79d0de6eeec3a1c6386e1dd0244b6`. Conversation watches already publish memory revisions before storage, acquire the held value, and avoid replaying old holds when a dependent transaction or timer stores them. No production fix was needed.

The exact source is official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`: `packages/durable/src/harness/view.ts:141-156,325-391` and the replay/operation requirements of `packages/durable/test/harness-view.test.ts:128-158`. No upstream TypeScript execution or additional complete `it` body is credited.

### Controls and adaptations

`durable/view_deferred_memory_main_test.go` uses a real standalone Session with captured host timers and a ConversationHandle over that Session. The handle calls the production `Watch`/`View` paths; it has no task scheduler. Deferred Harness policy and generation wiring are still disabled.

* `TestMainSessionConversationMemoryDelivery` runs timer, read-dependent entry and changed-dependent entry scenarios on memory, journal and SQLite. The literal `s/docs/pi.live/generation/{attempt:1}` frame arrives while stored observers and the real backend still have no attempt. A second watch acquired during the hold sees that memory revision. Dependent transactions emit only their own mounted changes and entry append; the storing timer emits no extra view frame. Every delivered frame replays to its value, and the last value equals a fresh view and the stored live document after flushing.
* `TestMainSessionConversationMemoryBacklog` queues three held string appends before starting the watch, stores them using the actual timer, then receives one folded `a/docs/pi.live/text/abc` frame. Replay checks detect reapplication of the stored hold (`abcabc`). The backend independently contains `abc` before delivery starts.

Callback receipts preserve separate foreground frames in the first test. The backlog test has no callback acknowledgements between its commits. Draining checks the actual subscription's pending/running state with a bounded wait; it uses no guessed negative-delivery delay. Native detached maps, callback timing, DTO-number representation and the standalone handle are explicit adaptations. Shared JS identity, synchronous public view observers and scheduler wake/abort ordering are outside this slice.

An initial compile error called `DocumentHandle.Value` instead of `Get`. Initial literal comparisons also distinguished Go integers/typed maps from the decoded JSON numbers/maps. The expected operation tuples now pass through `ownOperations`, preserving paths, values, array lengths and operation order. Those harness failures are excluded from runtime failure counts. The corrected unchanged-runtime baseline passes all 12 backend leaves in 0.202s.

### Focused verification

Go 1.27.3, GOMAXPROCS=4; the protected continuation prototype is excluded using both logical and canonical overlay paths:

```sh
CGO_ENABLED=1 go test -overlay=<owned-run>/overlay.json ./durable \
  -run '^TestMainSessionConversationMemory' -count=3 -shuffle=42 -timeout=90s
CGO_ENABLED=0 go test -overlay=<owned-run>/overlay.json ./durable \
  -run '^TestMainSessionConversationMemory' -count=3 -shuffle=42 -timeout=90s
CGO_ENABLED=1 go test -race -overlay=<owned-run>/overlay.json ./durable \
  -run '^TestMainSessionConversationMemory' -count=3 -shuffle=7 -timeout=90s
```

Normal runs pass in 0.536/0.203s; race passes in 2.061s. Durable vet and Staticcheck v0.8.2 pass both CGO modes; module verification, formatting, diff and prototype-content checks pass. Staticcheck uses a physical package copy without the prototype; missing local inference/image/transport imports in the initial copy were restored before passing credit, with no finding suppressed.

Four active mutations each fail three backend leaves: omitted memory frames, folded durable changes shown as own view changes, timer storage delivered again, and late acquisition using the stale stored image. The timer mutation produces `abcabc` and fails replay. Mutation anchors were corrected before any credited mutant execution; no build failure counts as a rejected mutation.

Only the current release's harness-view review/generated crosswalk evidence cell changes. Inventory markers, dispositions, historical review files and the schema dependency queue are unchanged. Completed generation, observer, retention, lifecycle and deadline gates were not rerun. No production/dependency/coalescer change, full suite, profiling, hosted workflow, release ref or asset change. Raw sources, overlays, mutations, copied packages, logs and completed build cache are disposed after analysis; installed dependencies/toolchains and the protected prototype are preserved.

Next: fractional/extreme persistence windows, duration conversion and full-width clocks before public persistence/Harness wiring. The unchanged full gated-generation persistence oracle retains its known failing baseline; complete 1.5.0 alignment is unverified.
