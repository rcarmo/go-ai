# Native dependency correspondence: retained inputs

[`native-inputs.jsonl`](native-inputs.jsonl) joins selected retained Oracle files to the [pinned dependency graph](README.md), [archive receipt](archives.md), and focused native evidence. The Oracle is pi v1.6.0 at `faac585884ae2c013a6b1400f3e9e77f10b34bac`; the native evidence base is `bf65050e8e4cb868ded06b6dac362bf5bfc2684e`.

The verifier compares actual retained bytes and checks source locators against pinned Git. For prior focused runs, it cites the run receipt and traces the corresponding source loader; **the historical receipts are not module-load traces**. In particular, a prepared `dist` file is not proof that a test imported it rather than source. Native module versions and `go.sum` entries match at the respective tested commit and the evidence base. That is declaration evidence, not a new native build or verification of each Go module archive.

| Input | What matches or corresponds | What the tests establish |
| --- | --- | --- |
| QuickJS-WASI `3.6.2` | The archive's `quickjs.wasm`, the retained Oracle input, and the native WASM checked in at both revisions are identical. The native host uses `github.com/tetratelabs/wazero v1.12.0`. | `codemode.test.ts` constructs the upstream sandbox; the source loader resolves that WASM by default. The verified native QuickJS/SQLite reopen probe checks namespace identity, confirmed effect replay and live authority. This does not accept every worker, prelude or replay contract. |
| Photon `0.3.3` | The verified archive's JS glue and WASM match the retained Oracle files. `images-fit.test.ts` initializes Photon and its Node image processor. Native fitting uses Go image operations, with `golang.org/x/image v0.47.0` for BMP/WebP support. | All 11 unchanged originals passed; the native receipt verifies selected image outputs and settlement, including dimensions, MIME handling and diagnostics. It does not show identical algorithms, pixels for every format, or all platform engines. |
| Published workspace `dist` and Vitest `4.1.12` | The selected AI and Chord `dist` members and the Vitest launcher and CLI match retained Oracle bytes. The historical setup receipt documents the published-dependency staging and the locked Vitest replacement. | The setup receipt covers the named AI and Durable focused suites only. AI exports its package entry from `dist`; applicable `source` conditions and aliases can select source for other packages. Native uses the CGO-free Make/Go test path, not Vitest. The selected bytes do not cover the runner's transitive modules. |
| Durable SQLite layout | The upstream schema and migration source files are byte-identical to v1.5.2. Regenerating the v1.6.0 SQL statements with the retained extractor exactly matches the native embedded JSON. Upstream opens through Chord's `node:sqlite`; native uses `modernc.org/sqlite v1.61.1`. | The focused receipt covers copy-source refusal and SQL rollback, including original-cause handling. The byte check covers migration statements, not every runtime transaction, historical upgrade, viewer, or supported host. |

## Reproduce offline

```sh
ledger=docs/upstream-v1.6.0/dependencies
bun run "$ledger/verify-native-inputs.ts" "$pinned_git_mirror" "$oracle" "$release_archive_dir" \
  "$HOME/.npm/_cacache" "$project_npm_cache/_cacache" > /tmp/pi-v160-native-inputs.jsonl
cmp "$ledger/native-inputs.jsonl" /tmp/pi-v160-native-inputs.jsonl
```

The script reads the stated Git objects and retained files, verifies compressed archives before comparing their selected members, then runs the checked-in SQL extractor with Bun's `source` condition. The checkout and Oracle need their existing workspace links and files. No tests, installs, or network requests are made.

## Remaining decisions and evidence

- This slice's two cache searches found neither Google GenAI `2.32.0` nor OpenAI `7.59.0`. The [separate provider archive receipt](provider-archives.md) later verified both compressed archives; neither receipt establishes which bytes earlier tests loaded.
- Record the loaded module and executable provenance for future focused runs, including the Node and built-in SQLite versions and transitive Vitest files. Expand native and source mapping from these slices before claiming D26 closure.
- Confirm the required platform and credential matrix, then run the separately authorized acceptance and profiling gate against the exact candidate. No additional skips, hosted CI, tags or release are authorized by this receipt.
