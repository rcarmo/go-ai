# Durable edit paths and mutation serialization

The [Oracle receipt](edit-path-tools.json) records **six passes, zero failures and zero selected skips** from the unchanged pi v1.6.0 `packages/durable/test/tools.test.ts`. It covers edits through regular symlinks, distinct environment objects and filesystem identities, a new file under an aliased directory, and a literal backslash filename on Linux. The probe verifies exact Git source, the pinned test hash and the pi-ai archive. It uses retained dependencies, no credentials and no real shell-tool invocation.

Node's symlink API requires full Node-level file permissions. This mode instead mounts the private scratch directory and the read-only system runtime in a Bubblewrap namespace. The workspace and home directories are not mounted. The namespace has only loopback; both IPv4 and IPv6 egress probes returned `ENETUNREACH`. This is a Linux fixture receipt, not a Windows or macOS result.

Changing the scratch queue helper to split a literal backslash from the basename failed exactly the pinned missing-file case at line 513. All five other selected cases passed. The receipt reproduced byte-for-byte.

## Native comparison

[`TestSourceV160EditPathSerialization`](../../../durable/tools/edit_path_upstream_test.go) invokes the real Go `ReadTextFile`, `Edit`, and `Write` paths with local files and symlinks contained in temporary directories. Channel barriers hold actual reads or writes while the next call resolves its path. A lock-table observation checks the held canonical key before release. The tests compare mutation order and exact file contents without assuming a competing call runs within a sleep interval. The source's direct queue case is exercised by two public writes on the same file in Go.

Five cases already passed. The compiled backslash case acquired a different key before creation than after creation. The [queue helper](../../../durable/tools/filesystem_mutation.go) now derives the filename from the parent returned by the filesystem instead of assuming both slash styles are separators. This changes the lock key only, not which path the tool opens. Path resolution, allowlists, and symlink-following rules are unchanged.

Restoring the old Go key logic failed only the backslash case. Focused Go tests passed twice after restoration with CGO disabled; vet, Staticcheck `0.8.2`, ShellCheck, and diff checks passed.

## Cases still open

Together with the [file](portable-file-tools.json), [Bash](linux-bash-tools.json), and [mock PowerShell](mock-powershell-tools.json) receipts, this packet covers **30 distinct originals of 36**. The new receipt names all 30 unselected cases; 24 already have those earlier receipts. The other six are open, not excluded: the two GIF signatures, image output by content, delayed abort during write, delayed abort during edit, and real Windows PowerShell.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-portable-file-tools.sh" "$pinned_git_mirror" "$retained_oracle" "$release_archive_dir" edit-paths \
  > /tmp/pi-v160-edit-path-tools.json
cmp "$inventory/edit-path-tools.json" /tmp/pi-v160-edit-path-tools.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run "TestSourceV160EditPathSerialization|TestFileMutation|TestSerializedMutation|TestConcurrentEditsOnlyOne|TestMutationLockCleanup|TestCustomFilesystemFactoryProductionWriteEditReadNoHostIO" -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle probe acquires the shared lock itself. The `edit-paths` mode requires Linux with unprivileged namespaces and Bubblewrap.
