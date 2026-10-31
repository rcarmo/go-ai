## Vertex credential-file admission

Unix credential opens now use `O_NONBLOCK`, and the reader checks the opened descriptor's type and size before reading. A path swapped from a regular file to a FIFO can no longer block in `Open` while the stream is cancelled. Regular-file symlinks still pass admission.

The previous path-level `Stat` checked a different object from the later `Open`. Two regression cases replaced the owned path with a FIFO between those calls: one with a live context, one cancelled inside the open hook. Both blocked past the one-second retirement deadline. An owned RDWR FIFO peer then released each reader; the stream and collector finished before cleanup. The live-context case also reached JSON parsing instead of rejecting the opened non-regular descriptor.

`vertex_file_unix.go` opens nonblocking; `vertex_file_other.go` retains the native opener on other platforms. Common code checks cancellation immediately after open, then uses `file.Stat` to enforce regular-file/4 MiB bounds on the actual descriptor. All failure paths close the descriptor. Bounded reads and the existing post-read cancellation check are unchanged. No file-reader goroutine or background refresh worker was introduced.

## Tests

Seven new cases pass:

| Tests | Result |
| --- | --- |
| Linux FIFO swap, live/cancelled context | One error/aborted assistant terminal; no inference and no blocking open |
| Regular descriptor | Reaches credential-type validation, then closes |
| Directory / oversized replacement after path Stat | Rejects the opened object before reading or creating a token client |
| Cancellation after open | Closes the descriptor and returns cancellation before token I/O |
| Linux regular-file symlink | Reaches credential-type validation through the symlink |

The FIFO cases failed on the earlier opener. Four descriptor cases also assert that every captured file is closed; their token-client tripwire forbids network I/O. The symlink case tests file admission, with full token acquisition covered by the existing service-account and authorised-user tests. All filesystem objects live under the project-owned run directory.

Root normal tests, Google race, root vet, Google-scoped staticcheck v0.8.2, module verification and the vulnerability gate pass with Go 1.27.3/GOMAXPROCS=4. Google package cross-builds pass for Windows/amd64 and Darwin/arm64 with CGO disabled; no runtime checks were run on those platforms. Dependencies, accepted SBOM/assets, catalogues and v1.4.0 are unchanged. Disposable test output was removed after use.

## Cancellation limits

The fix addresses Unix FIFO replacement and descriptor admission. `O_NONBLOCK` does not impose a deadline on filesystem metadata or regular-file syscalls. Context is checked before path inspection, after open and after the bounded read; stalled kernel/network-filesystem calls can still delay cancellation. In-place writes can also race with reads. The adapter has no atomic snapshot guarantee for those writes.

The SDK accepts a wider file domain than the native regular-file/4 MiB boundary; full disposition of that domain and non-Unix special-file behaviour requires review. [Known-file ADC discovery](vertex-default-adc.md) now uses these same admission checks. Metadata/GCE and other credential types, broader SDK convergence, acceptance and CPU/allocation profiling are incomplete.
