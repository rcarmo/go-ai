# Durable Bash tools: isolated Linux originals

The [Oracle receipt](linux-bash-tools.json) records **eight passes, zero failures, and zero selected skips** from the unchanged `bash` group in pi v1.6.0 `packages/durable/test/tools.test.ts`. It verifies the pinned test and related source bytes against the test inventory and uses the verified pi-ai release archive. No real provider or credentials are used.

The runner and its shell children use an unprivileged Linux network namespace with only `lo` and no network administration capabilities. Both documentation-address probes returned `ENETUNREACH`. Node also has no network permission. The original tests use short local commands and mock environments; this is IP network isolation, not a claim about other IPC or access to arbitrary host files. The cwd test followed the original's POSIX branch. Raising only the reported nonzero-exit diagnostic by one failed the unchanged assertion at line 666; the other seven still passed.

## Native comparisons

[`TestSourceV160LinuxBash`](../../../durable/tools/linux_bash_upstream_test.go) drives the real Go `Bash` registration and local Linux shell, with the environment's output callback recorded. The timeout-spill case injects the same kind of environment failure used by upstream. These comparisons all passed before any runtime change, so none was made.

| Source case | Native scope |
| --- | --- |
| 628: retained window and skipped bytes | Oracle only. The Go retained-output window and counters are owned by the Harness, not the standalone call used here. |
| 654, 662: streams, nonzero exit, timeout | Exact captured bytes, exit code, nonzero error wording and timeout cause. Partial: Go's standalone call returns `Content`, `Details` and a Go error instead of the original's `structuredOutput`, `isError` and diagnostic envelope. |
| 676: spill after timeout | Exact timeout error; resolve the spill notice and read the entire expected file. |
| 691: prepared command, cwd and environment | Exact prepared command/context, stdout, isolated explicit environment and working-directory marker. Partial: this seam has no Harness `ToolAPI` object to compare with the source's callback identity. |
| 725, 734, 748: prefix, full spill, small output | Exact prefix output, complete streamed and spilled bytes, and no small-output spill or notice. |

Applying the equivalent Go exit-diagnostic mutation failed only `nonzero-and-timeout`. Restored Go tests passed twice with CGO disabled and the test binary in the same loopback-only namespace. Vet, Staticcheck `0.8.2`, ShellCheck, and diff checks passed. Both this receipt and the earlier [portable file receipt](portable-file-tools.json) reproduced byte-for-byte.

## Cases still open

The two receipts cover **21 distinct originals of 36**. The new receipt names all 28 unselected cases, 13 of which already have the portable-file receipt. The 15 other originals remain open, not excluded:

- Three image-signature or image-output cases.
- Two delayed abort/mutation-queue cases and six edit path/serialization cases, including the non-Windows backslash-name case.
- Four PowerShell cases: three mocked adapter cases and the Windows-only real process.

No original in the selected group asserts stdin, descendant cleanup, or a Windows shell; this packet makes no claim about them.

## Reproduce offline

```sh
inventory="$PWD/docs/upstream-v1.6.0/test-inventory"
bash "$inventory/probe-portable-file-tools.sh" "$pinned_git_mirror" "$retained_oracle" "$release_archive_dir" linux-bash \
  > /tmp/pi-v160-linux-bash-tools.json
cmp "$inventory/linux-bash-tools.json" /tmp/pi-v160-linux-bash-tools.json

source scripts/project-env.sh
source "$inventory/offline-loopback.sh"
scratch=$(mktemp -d /tmp/v160-linux-bash-native.XXXXXX)
trap 'rm -r -- "$scratch"' EXIT
mkdir -p "$scratch/home" "$scratch/tmp"
prepare_offline_loopback "$scratch"
printf '#!/usr/bin/env bash\nset -euo pipefail\nsource %q\nrun_offline_loopback %q "$@"\n' \
  "$inventory/offline-loopback.sh" "$scratch" > "$scratch/run-native.sh"
chmod 700 "$scratch/run-native.sh"
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS="-exec $scratch/run-native.sh -run TestSourceV160LinuxBash -count=2 -shuffle=42 -p 1 -timeout 90s"
```

The Oracle script takes the shared lock itself. Omitting `linux-bash` reproduces the earlier file-tool receipt.
