## Persistence windows and full-width clocks

Native runtime `b1502fba4d26dd6253ca17a699c063ae6dcea58c` fixes deadline precision and premature duration saturation. Base/rollback is `e58e0b9a88c770ce38a3d733e9b632317d9f310d`. Public persistence options and Harness generation policy are still unwired.

Source: official v1.5.0 `f9a26365568127461c47a07a37dcb42c140a3ac5`, `packages/durable/src/session/session.ts:173-187,517-525,1223-1235,1297-1323`. These define nonnegative numeric windows, configured-default fallback, earliest absolute deadlines and nonnegative remaining delays. The original source tests cover negative/NaN inputs, integer deadlines and infinity; the added numeric extremes are source-derived controls and native clock extensions. No additional complete upstream test body is credited.

### Numeric boundary

`durable/persistence_deadline.go` retains upstream Number addition and subtraction when the native clock lies in JavaScript's safe-integer range. For example, clock `1700000000000` plus window `0.1` gives a remaining source delay of `0.10009765625` ms; the native host receives 100097 ns after truncation. A subnormal window at clock 1000 rounds to zero as upstream does.

Outside that range, the existing native `int64` clock can distinguish values that a JavaScript Number cannot. The deadline keeps its integer origin separate from the window; bounded standard-library rational arithmetic compares absolute deadlines and subtracts clocks without overflow or cancellation. Equal deadlines from different origins remain equal, including beyond `MaxInt64`; later windows cannot rearm an earlier hold. The full-width native cases extend the source clock domain and receive no same-input JavaScript parity credit.

Finite positive delays truncate fractional nanoseconds and saturate at `time.Duration(MaxInt64)`. The last whole millisecond below that ceiling, 9223372036854 ms, remains exactly 9223372036854000000 ns. Zero and negative zero are valid deferred windows and arm a zero-delay host timer; they do not force storage before it fires. Infinity keeps its no-timer meaning. Captured host fires still store every hold even if the clock has not advanced; no new clock recheck was added.

### Baseline and controls

Four functions in `durable/session_deferred_numeric_main_test.go` exercise the real private Session commit, host timer and backend paths. They cover zero/signed-zero/fractional/subnormal windows, duration limits/largest finite window, full-width clocks, earliest/later deadlines, source Number rounding, invalid explicit windows before the callback and invalid configured-default fallback.

The unchanged-runtime baseline fails 12 backend leaves: three at the duration ceiling and nine at full-width clocks. The ceiling delay was 775807 ns too long. At clock `2^53+1`, a 1.25 ms window became 2 ms; near `MaxInt64` and at `MinInt64`, it became zero. The separately added source-Number rounding controls pass unchanged on all six backend leaves. No harness build or fixture failure was counted.

Two functions in `durable/persistence_deadline_test.go` compare ordering and delays against independent 2304-bit binary arithmetic (4096 seeded vectors, seed 6139) and check equal deadlines beyond the integer clock range, cross-range subtraction and saturation. Production uses rational arithmetic. The reference explicitly applies the source Number operations within their domain, then computes the native duration boundary independently. All comparisons pass exactly; no numerical tolerance was widened.

An isolated execution of the actual upstream `persistWithin` function passes six default cases. The actual source deadline and delay expressions were also extracted and executed under Bun, confirming normal fractional rounding, subnormal loss and the Number-domain 2/0 ms results above. This executes neither the full Session nor Vitest.

### Focused verification

Go 1.27.3/GOMAXPROCS4, protected prototype excluded through logical and canonical overlay paths:

```text
-run '^(TestMainSessionDeferred(Numeric|Deadline)|TestPersistenceDeadline|TestMainSessionConversationMemory)'
normal count3/shuffle42: CGO1 1.956s; CGO0 0.795s
race count3/shuffle7: CGO1 4.774s
```

The existing six deadline functions and two conversation-memory functions were rerun because deadline representation/conversion changed. Other completed observer, retention, lifecycle and generation gates were not repeated. Durable vet and Staticcheck v0.8.2 pass both CGO modes; module verification, formatting, diff and prototype-content checks pass. Staticcheck uses a physical package copy excluding the prototype.

Six active mutations reject 9/3/3/9/3/3 backend leaves: rounded full-width origins, lost source Number rounding, early saturation, reversed deadline order, unchecked duration overflow and an infinity timer. All compile and fail their intended assertions. The overflow mutation reaches the host's zero-delay clamp; the infinity mutation violates the original infinity/release test. A final source-subtraction review was followed by fresh complete focused normal/race/static/mutation checks; the times above are the final results.

Only the official release's Session-deferred review/generated crosswalk evidence cell changes. Inventory markers, dispositions, historical review files and the schema dependency queue are unchanged. No dependency/coalescer/storage-format/public API change, full suite, profiling, hosted workflow, release ref or asset change. CPU/allocation cost of exact arithmetic has not been profiled; acceptance profiling remains a later authorised gate. Completed raw sources, overlays, mutations, copied packages, logs and build cache are disposed after analysis; installed dependencies/toolchains, native v1.4.0 and the protected prototype are preserved.

Next: public persistence/Harness ownership, reporting, waiter/abort and shared-value prerequisites, followed by the generation intent/presentation split. The full unchanged generation persistence oracle retains its known failing baseline; complete 1.5.0 alignment is unverified.
