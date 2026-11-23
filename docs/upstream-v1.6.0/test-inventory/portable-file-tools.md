# Durable portable file tools

The [Oracle receipt](portable-file-tools.json) records **13 passes, zero failures and zero selected skips** from the unchanged pi v1.6.0 `packages/durable/test/tools.test.ts`. The selector covers the no-environment case, six text reads, one write, and five edits. It checks exact Git bytes against the [Round 21 queue](primary-queue-r21.jsonl), uses the verified pi-ai release archive and retained dependencies, and runs Node without network permission. The selected cases do not run shell commands or require credentials.

The receipt names all **23 unselected cases**: three image cases, two delayed-abort mutation cases, six edit serialization/path cases, and 12 Bash/PowerShell cases. These include the non-Windows backslash-name case and the Windows-only real PowerShell case. The unselected group also includes cases that use mocks rather than real process starts. None is excluded. No image-preparation result or platform coverage is claimed here.

## Native comparison

[`TestSourceV160PortableFileTools`](../../../durable/tools/portable_file_tools_upstream_test.go) invokes the real Go tool registrations. It compares exact text, notices and truncation details, checks the actual bytes written, and checks edit repair without changing the caller's arguments. It reapplies the four-line unified patch independently without invoking a process. The growing-log test appends during real positional reads, without sleeps or timers. Go's `Content` field carries the text the original returns as a text block.

Eleven cases already passed. Two failed on the compiled baseline: an absent environment caused a nil-interface panic, and a read that ended at the line limit returned a nil diagnostic slice instead of an empty array. The [environment resolver](../../../durable/tools/env.go) now returns the ordinary error `No execution environment is configured`. Both Go text-read paths preserve an empty diagnostic slice when there are no notices. Path resolution, sandbox allowlists, traversal guards, mutation locking and image preparation are unchanged.

Increasing only the continuation notice's offset failed exactly the offset case in both runtimes (`62` instead of `61`). The Oracle receipt reproduced byte-for-byte. Focused Go tests passed twice with CGO disabled; vet, Staticcheck `0.8.2`, ShellCheck, and diff checks passed.

## Reproduce offline

```sh
inventory=docs/upstream-v1.6.0/test-inventory
bash "$inventory/probe-portable-file-tools.sh" "$pinned_git_mirror" "$retained_oracle" "$release_archive_dir" \
  > /tmp/pi-v160-portable-file-tools.json
cmp "$inventory/portable-file-tools.json" /tmp/pi-v160-portable-file-tools.json
source scripts/project-env.sh
flock /tmp/go-ai/v160-focused-verify.lock make test PACKAGES=./durable/tools \
  TEST_FLAGS='-run "TestSourceV160PortableFileTools|TestRead104BoundedProductionHeadAndDecoderBoundaries|TestReadReferenceWholeLineByteTruncationAndFirstLineDiagnostic|TestEditArgumentPreparationRepairsLegacyShapesWithoutMutatingProvenance|TestEditNormalizesLineEndingsPreservesBOMAndRejectsNoOp" -count=2 -shuffle=42 -p 1 -timeout 120s'
```

The Oracle probe acquires the shared lock itself.
