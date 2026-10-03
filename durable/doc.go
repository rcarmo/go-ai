// Package durable implements M1a's atomic native storage/session foundation.
// It does NOT yet run a model, accept a scheduling submission, execute a tool,
// recover an invocation or provide pi-durable full parity. M1b real generation
// and M1c owned tools/recovery are separate milestones. The storage union includes
// full passive task/submission records, not runnable task claims.
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
// No sidecar, rename, reclamation, delta or automatic corruption repair exists.
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
// Full bases replace current values; retired-ID metadata remains indexed. Heap
// overhead can exceed retained bytes; no hard RSS bound is promised. Journal
// growth/replay cost is append-only. Queries are detached committed snapshots,
// numeric ascending-ID pages except entry cursor ordered by commit/position.
// Forks, historical/asOf, deltas/copies and family APIs are unsupported.
//
// Records may contain sensitive user/model/tool data. Credentials, headers,
// clients and executable callbacks have no persistence fields; errors never
// echo payloads. The host must avoid placing secrets in application JSON and
// choose retention/encryption; owner-only permissions are not encryption.
//
// Interim upstream 23-case applicability ledger (13 complete / 7 partial /
// 3 unsupported) follows the accepted design, NOT a passing full-parity claim:
//
//	01 complete: reserved root1/immutable conversation creation.
//	02 complete: mixed atomic records and rollback (no task execution claim).
//	03 complete: detached retained writes/reads plus immediate Set/Update copies.
//	04 complete: prototype-like/NUL/whitespace JSON keys preserved as data.
//	05 complete: entries committed out of ID order.
//	06 complete: direct entry cursor stable with newer commits; no ancestors.
//	07 complete: ascending opaque conversation cursor.
//	08 partial: owner-edge/conjunctive raw scans; scheduler cascade later.
//	09 unsupported: deep fork ancestry (fork commands reject).
//	10 complete: full task replacement/filter scans; no scheduler.
//	11 complete: owner/waiting/completing raw statuses; joins later.
//	12 complete: local requestID index/replacement/cross-type conflict.
//	13 complete: passive-write submission storage union; inbox later.
//	14 partial: current-only retire/reincarnate; rewind/asOf later.
//	15 unsupported: delta tails (delta commands reject).
//	16 unsupported: document copy/ambiguous sources (copy commands reject).
//	17 partial: version base changes; historical/delta boundaries later.
//	18 partial: exact singleton scope/address; family/history later.
//	19 partial: atomic current lifecycle; historical membership later.
//	20 partial: supported-batch index rollback; delta/copy replay later.
//	21 partial: string identities/kind grammar; family keys later.
//	22 complete: global namespace and exact reservation/exhaustion policy.
//	23 complete: Store/Session/escaped-Tx close rejection; later runtime absent.
//
// Rejection tests for unsupported cases are labelled unsupported and never
// counted as passing their full upstream obligation. No hidden skipped tests.
package durable
