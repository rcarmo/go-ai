// Package durable implements native atomic storage and persistent model → owned
// host tool → answer runs through goai.Stream. Copied definitions, bounded schema
// validation, persisted final arguments and conservative replay guard effects.
// The M1 runtime is published; full pi-durable parity and deferred model requests
// remain incomplete. Entry ancestry and full-base document/fork semantics are
// independently focused-accepted, including definition/migration APIs. Final
// definition normal/race runs each passed 11 tests plus eight subtests, no skips;
// an independent public-API clone run passed 12 tests, 22 total passes, no skips.
// Integrated durable normal/race passed 103 tests plus 394 subtests each, no
// skips; full-project candidate gates have not run. Open reconciles running tasks to pending
// but dispatches no model effect; Submit, Resume and Wait can schedule work.
// Request intent pins a sanitized model behavior DTO, system/messages cutoff
// and curated Temperature/MaxTokens settings. Terminal identity/usage attribution
// comes from that pinned model, never from omitted/conflicting provider claims.
// Endpoint, headers and credentials are stripped before persisted DTO validation
// and resolve process-locally; private header sizes do not relax persisted caps.
// Other behavior options and payload-rewrite hooks reject. Read-only host observers have no purity guarantee. Close joins provider
// channels (including noncooperative ones); received terminal outcomes settle
// atomically even during Close, unfinished requests/tools remain pending for
// conservative recovery. Abort commits marks before signalling/joining children,
// whose terminal receipts precede parent cleanup. ToolAPI writes seal on return;
// outcome callbacks are limited to conversation-scoped non-pi application docs.
// Generic requests can be dispatched/billed again after uncertain remote outcome;
// no exactly-once billing or complete accounting of lost usage is promised.
// Wait cancellation only ends that wait. Committed terminal receipts, tasks,
// application docs, submission and pi.usage accounting are atomic. Selected
// bounded model partials/tool prefixes are explicit committed checkpoints; raw
// frames are never visible. Tool output is capped32KiB, registry/offered16 tools,
// rounds16. Replay requires stored/current safe with exact implementation/version
// and schema identity; unsafe/missing/changed code never silently reruns.
// pi.agent/pi.live/pi.inbox/pi.usage are version1 native bases; inbox placement
// covers follow-up/write, not full steering/reset/post-tools boundary parity.
//
// Transactions are synchronous, single-owner and callback-scoped. Set and Update
// strictly validate and detach JSON before returning. Retained inputs, Update
// candidates and returned snapshots are never staged-state authority. Methods on
// escaped Tx/DocumentHandle reject once sealed. Concurrent mutation DURING an
// input copy is unsupported Go data-race misuse; reentrant/overlapping methods
// reject. Taking an OpenSession claims the private native writer capability:
// retained Storage.Apply/MintID/Close reject, while detached committed reads are
// allowed. The Session mutation line covers preparation, settlement and adoption.
// Once append is admitted, caller cancellation cannot end settlement or release
// the line. Close fences admissions and drains that line; cancellation only ends
// that caller's wait for a common shutdown. No successor owns the file/image
// before Close finishes. Uncertain write/sync outcomes poison reads/mutations;
// only Close/reopen may resolve the confirmed prefix. Deterministic rejections
// before admission leave the store usable. Allocator reservations commit alone
// and remain spent even when the later transaction fails.
//
// Native journal v1 is one owner-only directory (0700) and journal.bin (0600).
// This is NOT the official JSONL/sidecar format. Frames are exactly 64-byte
// header + strict JSON payload + 8-byte ENDGOD1! terminator. Header: GODJNL1!,
// little-endian version uint16=1, type uint8(config0/reservation1/commit2), flags0,
// payload uint32 length, ordinal uint64, allocator high-water uint64 and SHA-256
// over the first32 header bytes plus payload. Config first/ordinal1/high-water1
// persists format, effective limits and Sync. Reservations advance high-water
// without logical Seq; commits advance Seq and preserve high-water. IDs/Seq/frame
// ordinals never exceed 2^53-1. Root1 is reserved; first minted ID2. Complete bad
// headers/checksums/JSON/relations fail closed. Only a legal final incomplete
// header/frame/terminator prefix is truncated; no magic scanning/repair occurs.
// No sidecar, rename, reclamation or automatic corruption repair exists.
//
// Sync defaults true. File sync follows append; directory creation syncs parent
// edges and new file directory metadata before acknowledgement. Unsupported or
// failed required directory sync rejects opening. Actual power-loss behaviour
// depends on the filesystem/device; process-crash ordering is not a universal
// power-failure guarantee. Sync=false omits those barriers and is persisted;
// conflicting explicit reopen options reject. In-process path/image exclusion
// is enforced through full close settlement. Cross-process/host exclusivity and
// exclusion of symlink/directory replacement are HOST preconditions; no OS lock
// or multiwriter guarantee is supplied. MemoryImage reopening retains RAM only.
//
// Defaults/hard v1 ceilings: frame4MiB/16MiB (plus72 overhead), record512KiB/1MiB,
// object256KiB/1MiB, decoded string/key256KiB/512KiB, requestID512/4096 bytes,
// depth32/64, members4096/16384, JSON nodes65536/262144, writes256/1024,
// page256/4096 (positive explicit limits), indexed records100000/1000000,
// retained encoded records32MiB/128MiB, file256MiB/4GiB. Config bootstrap16KiB.
// Limits are persisted, coherent and applied to writes/replay; strict bounded
// traversal precedes incremental encoding, which never calls caller marshalers.
// Latest full bases replace current values; rewindable bases retain each commit
// revision. Both revision counts and bytes consume persisted budgets; no history
// is silently pruned. Retired-ID metadata remains indexed. Heap
// overhead can exceed retained bytes; no hard RSS bound is promised. Journal
// growth/replay cost is append-only. Queries are detached committed snapshots,
// numeric ascending-ID pages except the legacy entry cursor ordered by
// commit/position. HistoryStorage supplies fork-aware newest-ID-first scans,
// stable query-bound cursors, visible entry lookup and inherited head markers.
// ParentAt is an inclusive entry cutoff; model context honours ancestry and the
// latest head. DocumentHistoryStorage adds half-open incarnations, rewindable
// base revisions, exact singleton/family addresses and pre-batch copies. Tx forks
// select asOf documents from the entry-owning ancestor and current documents from
// the immediate parent. The document/fork slice is independently focused-accepted.
// Native immutable document definitions add singleton/family initialization,
// guarded typed-policy acquisition, bounded migration caches and detached
// snapshots. Snapshot migration writes nothing; first successful typed Tx writes
// the required version base even without value changes. The definition slice
// is independently focused-accepted. Historical migrations enforce the document
// limit, including exact 512-byte acceptance and 513-byte rejection in the
// independent probe. The failed non-private journal test fixture is preserved;
// its private-child correction passed without relaxing runtime permissions.
// S2c2 decoded delta/checkpoint APIs are independently focused-accepted.
// Owner normal/race passed 17 tests plus 159 subtests each, no skips; independent
// clone/public probes passed 19 tests, 180 total passes, plus a distinct low-level
// history/copy/reopen test with two backend subtests. Operations
// r/s/d/a/t/p/m detach inputs and placements, reject unsafe paths and enforce
// persisted budgets at every intermediate revision. Front truncation counts
// UTF-16 units; a surrogate-splitting cut rejects under native strict Unicode.
// Ordinary nonempty changes use deltas unless the definition selects a base.
// Predicates receive detached final value/ops and stored deltas-since-base,
// excluding the current change; create/copy/required migration bases bypass
// predicates. Explicit operations preserve structural intent even when values
// compare equal. Native Set replaces the root; detached Update emits a bounded
// structural diff. No wire dictionary, JS proxy or canonical tuple is promised.
// Full M2-M4 parity and full-project candidate gates are unfinished. Legacy
// latest-only pi.agent metadata rejects an unprovable historical harness fork.
//
// Records may contain sensitive user/model/tool data. Credentials, headers,
// clients and executable callbacks have no persistence fields; errors never
// echo payloads. The host must avoid placing secrets in application JSON and
// choose retention/encryption; owner-only permissions are not encryption.
//
// Interim upstream 23-case applicability ledger (13 complete / 7 partial /
// 3 unsupported at M1; additions below are explicitly marked focused-tested.
// This is not a passing full-parity count:
//
//	01 complete: reserved root1/immutable conversation creation.
//	02 complete: mixed atomic records and rollback (no task execution claim).
//	03 complete: detached retained writes/reads plus immediate Set/Update copies.
//	04 complete: prototype-like/NUL/whitespace JSON keys preserved as data.
//	05 complete: entries committed out of ID order.
//	06 complete: direct entry cursor stable with newer commits; no ancestors.
//	07 complete: ascending opaque conversation cursor.
//	08 partial: owner-edge/conjunctive raw scans; scheduler cascade later.
//	09 focused-accepted: ancestry scans/heads/visible lookups/public fork copies;
//	   legacy-agent historical backfill still required for full parity.
//	10 complete: full task replacement/filter scans; no scheduler.
//	11 complete: owner/waiting/completing raw statuses; joins later.
//	12 complete: local requestID index/replacement/cross-type conflict.
//	13 complete: passive-write submission storage union; inbox later.
//	14 focused-accepted: half-open rewindable incarnations and delta history.
//	15 focused-accepted: decoded deltas/tails/counts; native strict Unicode
//	   and intermediate budgets apply. Complete storage corpus audit is later.
//	16 focused-tested: independent pre-batch copy/ambiguous source rejection.
//	17 focused-accepted: historical version bases and same-version delta tails.
//	18 focused-tested: exact singleton/family scope/address and history.
//	19 focused-tested: empty lifetime and historical membership.
//	20 focused-accepted: copy/base/delta lifecycle rollback and replay.
//	21 focused-tested: lossless empty/NUL/prototype-like family keys.
//	22 complete: global namespace and exact reservation/exhaustion policy.
//	23 complete: Store/Session/escaped-Tx close rejection; later runtime absent.
//
// Rejection tests for unsupported cases are labelled unsupported and never
// counted as passing their full upstream obligation. No hidden skipped tests.
package durable
