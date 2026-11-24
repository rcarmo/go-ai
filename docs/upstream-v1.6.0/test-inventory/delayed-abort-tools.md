# Durable delayed mutation aborts

The [Oracle receipt](delayed-abort-tools.json) records **two passes, zero failures, zero selected skips** from the unchanged pi v1.6.0 `packages/durable/test/tools.test.ts`. The exact selection is delayed abort during write (line 331) and during the write phase of edit (line 423). The probe verifies pinned source bytes and the pi-ai archive. It runs without credentials in a loopback-only Linux network namespace, with Node's file permission scoped to scratch and no Node network permission.

Releasing the scratch queue before awaiting the admitted mutation makes both cases fail at the assertions that the second mutation has not reached its host write (lines 345 and 438). The control does not change either original test.

## Native comparison

[`TestSourceV160DelayedAbortMutationQueue`](../../../durable/tools/mutation_abort_upstream_test.go) invokes the public Go `Write` and `Edit` registrations and the local filesystem's `ReadTextFile` and `WriteFile`. Its fixture gates the actual host write calls. After canceling the first call, the test observes its held canonical key and a second same-key claim. It joins both calls and checks real file contents inside a temporary directory. Channels establish ordering; the timeout only bounds failure.

| Original | Native assertions |
| --- | --- |
| Write, line 331 | The second mutation stays off the host while the first is unsettled. The first call is canceled and does not create `file.txt`; the second succeeds and the file contains `second\n`. |
| Edit, line 423 | The second mutation stays off the host while the first write is unsettled. The first host write still commits `ALPHA\nbeta\n`, but the first call rejects with `Operation aborted` and retains its `context.Canceled` cause. The second edit succeeds and the file contains `ALPHA\nBETA\n`. |

The compiled baseline passed the write case and failed only the edit's diagnostic: it returned `context canceled`. The fix translates cancellation after a successful host write to the pinned message without changing queue, filesystem, or cancellation behavior. Temporarily releasing the Go queue before the host call made both tests fail. After restoring the queue, focused tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, ShellCheck, and diff checks also passed. The Oracle receipt reproduced byte-for-byte.

## Cases still open

This selection brings the five disjoint `tools.test.ts` receipts to **32 distinct originals out of 36**. The two GIF signature tests, returning an image by content, and real Windows PowerShell are unselected in this packet and have no receipt in that union. None is excluded.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-portable-file-tools.sh" "$pinned_git_mirror" "$retained_oracle" "$release_archive_dir" delayed-abort \
  > /tmp/pi-v160-delayed-abort-tools.json
cmp "$inventory/delayed-abort-tools.json" /tmp/pi-v160-delayed-abort-tools.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run "TestSourceV160DelayedAbortMutationQueue|TestSourceV160EditPathSerialization|TestFileMutation|TestSerializedMutation" -count=2 -shuffle=42 -p 1 -timeout 90s'
```

The Oracle probe acquires the shared lock itself.
