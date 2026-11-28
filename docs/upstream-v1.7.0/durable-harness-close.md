# Unchanged Durable harness close lifecycle

This isolated comparison selects the exact one-file original `packages/durable/test/harness-lifecycle.test.ts` from pi `fb0fe823539f6d04850f6e34748c01f01f212e1f`. It is the smallest inventory file whose name contains the close/lifecycle scope; the requested `harness-close.test.ts` is absent. Covered blocked-recovery and cancellation-barrier lanes were not selected again.

The original has 713 lines and 28,082 bytes. Its pinned Git blob is `9ef2063f0185bc551791c0fd7ac4534736de31da`; the SHA-256 of the detached file and Git object bytes is `1200987a0f39edfcdf5da4d2566aabd575de0267575a7dc4b4c7b36e8edcd9c8`. The native lane starts at unchanged main `b1801c447d1193d56e3fadba2ac5100593b76813`.

## Original result

Vitest 4.1.12 on Node 26.13.0 ran the byte-verified original once under the shared `/tmp/go-ai/v170-focused-verify.lock` with `NODE_OPTIONS=--max-old-space-size=650`: **17 passed, 0 failed, 0 skipped**. The official v1.7.0 pi-ai package archive supplied the build exports required by the workspace; its recorded SHA-256 was independently checked before extraction. The test file itself remained unchanged.

```sh
cd "$PI_MIRROR/packages/durable"
NODE_OPTIONS=--max-old-space-size=650 flock /tmp/go-ai/v170-focused-verify.lock \
  node ../../node_modules/vitest/vitest.mjs run test/harness-lifecycle.test.ts
```

## Native controls

The unchanged native controls use public `Harness`, `ConversationHandle`, watch, task and production SQLite APIs where the contract is observable in Go. The focused selector covers failed-open cleanup and primary-error precedence, host/tool join, provider-session cleanup scope, cancelled close, same-file reopen, admitted commit settlement, close admission fences, queued cancellation, paused/read-only admission, progress scheduling and late registry replacement.

The selector is deliberately narrower than a claim of one-to-one JavaScript assertion parity. The original's `MemoryStorage` internals, TypeScript private `SessionImpl` access, exact JavaScript object identities and Chord callback envelopes remain source-adaptation gaps. No alternate native backend or test-only storage contract is substituted: selected storage controls use SQLite only, under `CGO_ENABLED=0`.

The initial unchanged native baseline passed. No implementation mismatch was demonstrated, so no production fix is required for this packet. Existing neutral controls remain authoritative; no assertion or production behavior was weakened to obtain the receipt.

```sh
source scripts/project-env.sh
export CGO_ENABLED=0 GOFLAGS=-p=1 GOMAXPROCS=2 GOMEMLIMIT=650MiB
flock /tmp/go-ai/v170-focused-verify.lock \
  go test ./durable -run 'SELECTOR' -count=1
```

`SELECTOR` is recorded in the accompanying assertion ledger's native-test table. The final focused run is repeated twice as part of this bounded comparison, followed by `go vet ./...` and Staticcheck. This packet is not full acceptance, acceptance profiling, a release gate or a push.
