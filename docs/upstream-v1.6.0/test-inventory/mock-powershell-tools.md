# Durable PowerShell tools: mocked adapter originals

The [Oracle receipt](mock-powershell-tools.json) records **three passes, no failures and no selected skips** from the unchanged pi v1.6.0 `packages/durable/test/tools.test.ts`. The selected cases record argument vectors, fallback and the last start failure, and a nonzero result. The source uses a mock environment for all three; no PowerShell executable or Windows host was used. The probe verifies the exact pinned source and the pi-ai archive. It runs in the same Linux loopback-only namespace as the [Bash packet](linux-bash-tools.md); IPv4 and IPv6 egress probes returned `ENETUNREACH`.

Changing the scratch UTF-8 console setting to ASCII failed only the exact argument-vector comparison at line 577. The new receipt reproduced byte-for-byte.

## Native comparisons

[`TestSourceV160MockPowerShell`](../../../durable/tools/powershell_104_test.go) uses the real Go PowerShell registration and an argv-only mock. It never starts a program. The UTF-8 text and the complete argument vector match the original, including the final command as one argument. The mock records fallback order, prefix application, and the exact last start error. Go preserved the last causal error but reported only `durable: shell spawn_error` on the compiled baseline. The [adapter](../../../durable/tools/powershell_unix.go) now exposes the last failed start's message while retaining that cause. It still falls back only on start failures.

The nonzero case is partial: it checks `partial`, exit code `3`, exact error wording and no fallback, but the standalone Go API uses text, details and a Go error instead of `structuredOutput`, `isError`, and the source diagnostic envelope. That result-shape difference remains open. The equivalent Go ASCII mutation failed only the argument-vector test. After restoration the focused tests passed twice with CGO disabled. Vet, Staticcheck `0.8.2`, ShellCheck and diff checks passed. No filesystem guards changed.

## Cases still open

The [file](portable-file-tools.json), [Bash](linux-bash-tools.json) and new receipts cover **24 distinct originals of 36**. Of the 33 unselected names in this packet, 21 have earlier receipts. The other 12 are three image cases, two delayed-abort mutation cases, six edit path/serialization cases, and the Windows-only real PowerShell case. None is excluded.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-portable-file-tools.sh" "$pinned_git_mirror" "$retained_oracle" "$release_archive_dir" mock-powershell \
  > /tmp/pi-v160-mock-powershell-tools.json
cmp "$inventory/mock-powershell-tools.json" /tmp/pi-v160-mock-powershell-tools.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run "TestSourceV160MockPowerShell|TestPowerShell104DirectArgvPreparationFallbackAndFailures" -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle probe acquires the shared lock itself.
