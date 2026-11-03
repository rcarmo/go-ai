## Pure-Go storage checkpoint

SQLite now runs under `CGO_ENABLED=0`, with focused memory/journal/SQLite conformance and fault checks passing. All aspects of official upstream v1.5.0 at `f9a26365568127461c47a07a37dcb42c140a3ac5` remain in scope. Full alignment and release acceptance are unverified; see the [release inventory][release].

### Adapter and preserved contracts

The [adapter][adapter] uses `modernc.org/sqlite` v1.61.1 and its selected `modernc.org/libc` v1.79.0 dependency. Modernc translates C to Go; the adapter needs neither CGO nor a system SQLite library. It preserves the native v1.4 storage API and `config`/`frames`/`checkpoint` schema, including reservation/commit frames, persisted limits and checkpoints. Upstream SQLite facade, physical schema and migration compliance still need auditing.

Cancellation controls admission. Admitted SQL settles with a background context on one pinned connection. Confirmed rollback rejects without adoption; uncertain commit or failed rollback poisons the store. Close drains admitted work, then closes the connection and pool before releasing the lifetime inode lock. URI encoding keeps `?`, `#`, `%`, spaces and Unicode in literal filenames, preventing filename text from selecting database options.

The driver documents 20 OS/architecture pairs. Native lifetime locking is [unsupported on non-Unix platforms][lock]. A private [connector boundary][connector] prevents the engine's platform restrictions from breaking unrelated memory/journal imports. Where the engine has no generated code, `OpenSQLite` returns `ErrUnsupported` before filesystem changes; SQLite is unavailable there. Required SQLite assertions remain visible and are not skipped. This boundary preserves package portability; wider-platform SQLite and secure locking remain open work.

### Focused checks — 2–3 November 2026

Used Go 1.27.3, `GOMAXPROCS=4`, `CGO_ENABLED=0` and `scripts/project-env.sh`. The candidate is based on native `375386c80475e80d879083c410b2a4e0368dc7e1`. Its final isolated source copy contains the SQLite/build-policy changes, with the four continuation runtime files restored to HEAD and continuation/prototype tests excluded **only from that copy**. The actual working-tree WIP and protected prototype were unchanged.

| Check | Result |
| --- | --- |
| Recovered pre-boundary focused gate, `^(Test.*SQLite.*\|TestInternalGroup.*)$`, count 3, shuffle 42 | 42 names, 126 top-level passes, zero failures/skips; 6.960s. Recovered after interruption without rerunning. |
| Post-boundary adapter tests, `^TestMainSQLitePureGo`, count 3, shuffle 42 | Four names, 12 top-level passes; 1.192s. |
| Final isolated candidate, same 42-name selection, count 3, shuffle 42 | 126 top-level passes, zero failures/skips; 7.820s. |
| Final three changed child/profile-configuration tests, count 3, shuffle 42 | Nine top-level passes, zero failures/skips; 0.306s. No capture path executed. |
| Final isolated `go vet ./durable` / staticcheck v0.8.2 | Pass; 1.744s / 4.637s after the final profiling-policy change. |
| Production `durable` dependency closure | No `CgoFiles`. |
| `go mod tidy -diff` / `go mod verify` | No remaining differences / all modules verified. Added only tidy-required checksums beyond the selected driver graph. |
| Linux/mips64le regression | Unconditional import failed compilation in modernc libc. Connector boundary restores package and test compilation (1.893s / 12.147s); no modernc packages enter this target's production closure. Cross-built tests were not executed. |

The 42-name selection includes three direct returns from `TestGroupSQLiteCrashChild` without its activation environment. Those returns add no crash coverage. Its parent exercises six real crash cases per repeat—two prefixes across three phases—for 18 subprocess cases. Other selected tests exercise real process exit without Close, cross-process ownership, transaction rollback, uncertain commit, checkpoint/reclaim, prefix isolation, task snapshots and persistence. Group tests retain memory, journal and SQLite cases.

The [adapter-specific tests][tests] verify literal filenames/inode ownership, WAL, `synchronous=FULL`, `busy_timeout=5000`, reclaim and reopen. Literal v1.4 schema/JSON fixtures exercise frames and checkpoint-plus-tail replay, reservations and subsequent writes. An actual old binary was not used.

The SQL-contention test holds a real writer lock through an independent connection. Admitted Apply still settles after caller cancellation. Queued reads and Close waiters return cancellation/deadline errors while background Close keeps admission sealed and retains inode ownership. After releasing SQL contention, Close drains, physical connections close and reopening recovers the write. Both cancellation modes run on real SQL; `busy_timeout` stays 5000ms.

Removed unconditional CPU/heap capture from the ordinary process-exit and ownership tests. Their actual child execution, exit code 23 without Close, reopen assertions and ownership rejection remain intact. The explicit `scripts/test-profile.sh` path sets `GO_AI_PROFILE_CHILDREN=1`; these children then retain CPU/heap capture, cumulative CPU/`alloc_space`/`alloc_objects` analysis and prompt disposal. The default-disabled configuration and shell syntax were checked; the opt-in capture path was not executed under Rui's profiling restriction. No profiling, full suite, race detector, hosted CI or release operation ran for this checkpoint.

### Dependencies, licences and security

Fresh SumDB-authenticated downloads matched the selected SQLite/libc module and manifest checksums. go-licenses v1.6.0 classified mathutil's BSD licence as unknown; pinned `github.com/google/go-licenses/v2@v2.0.1` recognises the unchanged licence. The full import graph including tests passes the unchanged six-licence allowlist. Assembly-inspection warnings remain. [Third-party notices][notices] identify the SQLite, translated musl and runtime licences; redistribution still needs their required full notices.

Pinned govulncheck v1.7.0 and the existing policy report **no reachable vulnerabilities for Go 1.27.3** on the isolated candidate. The independent whole-module OSV lookup includes `GO-2026-5320` in goldmark and `GO-2026-5932` in x/crypto; affected packages are absent from the checked import graph. The selected SQLite/libc pair had no OSV matches. This is not exhaustive embedded SQLite/musl or cross-platform vulnerability coverage.

A disposable CycloneDX v1.12.0 SBOM passed the existing guest, graph, schema, checksum and path checks: 40 components, labelled `375386c80475-sqlite-wip`. It was not a release asset. Raw logs, profiles, test binaries, copied sources and temporary SBOM output are deleted after analysis; concise results remain here.

### Remaining alignment

* Audit public `Storage` commit receipts/custom backends, upstream SQLite facade/schema/migrations, JSONL versus the native binary journal, `StoreStorage`, and IndexedDB/Cloudflare counterparts. Platform differences need explicit mappings and controls.
* Provide or explicitly resolve SQLite/secure-locking coverage outside the current supported targets. Cross-compilation alone establishes no runtime durability or isolation evidence.
* Finish explicit-continue and broader Session/Harness contracts separately; continuation WIP is excluded from this checkpoint.
* Code mode continues to use `internal/quickjs` through wazero (`durable/codemode_quickjs.go`); its wider v1.5.0 contract audit is unfinished.
* Pre-release CPU, `alloc_space` and `alloc_objects` analysis, measured-outlier tuning and broader acceptance gates await authorisation. The CGO-dependent race detector is unavailable; ordinary/profiled tests provide no substitute coverage.

[release]: README.md
[adapter]: ../../durable/storage_sqlite.go
[connector]: ../../durable/storage_sqlite_driver.go
[lock]: ../../durable/storage_lock_other.go
[tests]: ../../durable/storage_sqlite_purego_main_test.go
[notices]: ../../THIRD_PARTY_NOTICES.md
