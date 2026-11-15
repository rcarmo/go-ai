# Internal task replacement

Implementation checkpoint: `6c6758bf41812bf594822fb0b102eeb3c12f9f82`, pushed to main. Target: official pi-durable v1.5.0, `f9a26365568127461c47a07a37dcb42c140a3ac5`.

The private `Tx.setTask(TaskRecord)` follows the internal Transaction operation. The source public Tx interface has no setTask method; Go exposes no public replacement API.

## Source and implementation

Pinned `packages/durable/src/session/transaction.ts546–559` copies the complete replacement, rejects an existing staged terminal candidate or changed staged conversation, and does no committed lookup. Lines1163–1177 select staged timestamps, then supplied timestamps, then the clock when entering running/terminal state. Lines957–1055 validate fork plans, final owners and committed replacements before document selection/checkpoints and storage admission.

The Go setter keeps owned candidates separately until assembly. Assembly rejects missing committed tasks, terminal committed tasks and changed committed conversations, and validates owners against final candidates. Conversation allocation and document acquisition see staged task candidates. Task-scoped acquisition now retains the source missing/terminal error text. Existing terminal task-document retirement processes the converted records.

Complete replacement can change kind, version, input, background, abort mark, state and memos. Canonical Storage therefore no longer applies private runtime capability restrictions to these records. Existing private runtime paths retain those restrictions at staging/finalisation. Native task definitions, nested-tool forgery and raw Storage controls pass.

## Verification

Eight tests are consolidated in `durable/transaction_tables_test.go`:

- Original session-tables333–354 state/memo replacement packet.
- Source-derived deferred missing-record failure and staged terminal/conversation guards.
- Final abort-marked/terminal conversation owners, atomic rejection and fork-error precedence.
- Complete canonical metadata, including null input and exact stored JSON.
- Original session-tables390–410 terminal-candidate document acquisition/mutation subset; family variants and the full document-lifetime packet are not accepted by this subset.
- Owned nested memo JSON and staged timestamp precedence; omitted committed fields are not implicitly merged.

Initial controls failed before the setter existed. The fork-priority control then failed because missing-task lookup masked the fork error; complete metadata failed with `task identity changed`; document acquisition failed on native error text. Each passed after its corresponding change. Three final shuffled repetitions pass0.897s. Task-definition controls pass2.158s; nested-tool authority and raw Storage controls pass three repetitions0.260s/0.387s.

Full repository `make test PACKAGES=./... TEST_FLAGS='-count=1 -p 1 -timeout 1200s'` passes26 tested packages: durable746.181s, tools27.556s, public consumers13.197s. Vet, Staticcheck v0.8.2, logging and parity register pass. Five independently compiled faults are detected: omitted candidate, discarded staged timestamp, delayed fork validation, restored native replacement restriction and omitted abort-marked owner guard.

Selected original oracle:279 passing cases, zero skipped/todo/filtered;200 unchanged cases plus79 independent probes. The table-only run passes12 original cases plus87 supplements (eight unchanged conformance cases and79 probes). A new supplemental clock fixture initially assumed the total commit clock-call count; both supplemental fixtures now count only setter calls inside the callback. No original assertion changed; the fixture failure receives no production-failure credit. Analysed scripts, overlays and logs were removed.

## Generic runtime integration and registry construction

TaskRuntime.Commit now replaces its canonical record through the internal setter. Source scheduler.ts2437–2457 clears the restart count on a changed committed state or a decided outcome; runtime performs this step explicitly before replacement. The existing restart-limit assertions exposed its omission during refactoring. Identical intent/memo commits retain their count.

Source harness/registry.ts10–26 installs builtin definitions when a registry is constructed. NewRegistry now holds the existing CompactionTask immediately. The token is initialised once lazily to avoid a Go static initialisation cycle through phaseSelection/NewRegistry; its identity and registration protections remain stable. Generation2/Tool3 definitions are still absent.

The foundation candidate passes all26 tested packages: durable891.676s/tools27.174s/public13.087s, vet/Staticcheck/logging/register and two compiled semantic faults. This candidate used a compiler overlay removing only the two new, uncommitted canonical builtin RED targets; all previously accepted tests and the new registry control ran. It is not a passing receipt for the current working tree or canonical builtin execution. The earlier candidate879.259s failed two unchanged restart-limit controls. After the source-derived fix, ten repetitions pass10.235s/2.422s; three runtime repetitions22.055s. Registry control first failed0.033s, then ten repetitions pass0.039s; compaction controls4.412s and three reopen repetitions0.313s pass.

Upcoming ordinary-suite tests remain red and uncommitted: fresh registry Generation2/Tool3 definitions/migrations, and imported generation1/2 no-model execution (each hits its bounded3s deadline). The independent pinned import probe passes; its table run100 cases has zero skips. Initial reference module/receipt fixture errors and an initially unbounded native wait receive no implementation-failure credit. No placeholder phase handlers or private-checkpoint fallback have been installed. Temporary compiler overlays and analysed logs/scripts were removed.

## Remaining scope

Generic TaskRuntime.Commit now uses the private source setter. Canonical builtin execution, migration and caller integration are incomplete. Full task/document/family, malformed-value timing, read-key and assembly-order assertion audits are incomplete. Shared publication-record identity and broader API/Storage/lifecycle/readonly acceptance are incomplete. The complete original oracle is unaccepted because image equality exceeds the bounded worker heap; live-provider skips are unverified. Pre-release CPU/allocation profiling and tuning have not been completed. This development checkpoint establishes no full parity or release acceptance.
