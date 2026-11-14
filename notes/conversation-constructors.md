# Conversation ownership constructors

Target: official pi-durable1.5.0 at f9a26365568127461c47a07a37dcb42c140a3ac5. Source transaction.ts348-418 creates conversations from ownership options, mints identity internally, derives a task owner's conversation from its latest candidate record, stages the conversation and runs host initialization before returning the record.

Public Tx.CreateConversation now takes the source ownership-options JSON object and returns (Conversation,error). The old public full-record/error-only constructor is private. The higher-level Harness.CreateConversation(ctx,change) is unchanged. Reserved-root/bootstrap and legacy builtin producers retain private already-assigned-record staging; those paths do not establish the new public constructor contract.

## Source assertions and implementation

Two tests first failed0.220s against the old signature. They port the constructor field assertions from original session-tables.test.ts136-181 and191-215: four distinct conversation/entry/task IDs, self head, the literal first entry and complete task JSON, stored/returned headed records, staged-owner metadata and stored child record. They retain the missing-owner error substring. The original publication-object identity, frozen-task replacement and terminal/abort-marked atomic ownership packets are not accepted by this subset; canonical SetTask remains open.

An initial fixture used stored.Record, although Storage.conversation returns the record directly; that compile error was corrected without implementation-failure credit. The first factory run then exposed the typed Go ID not being recognised by an existing JSON-number helper; the source task-owner test passed after accepting the actual public ID type.

Source #stageConversation checks the extracted taskId against undefined. A task ownership object with omitted taskId remains ownerless; explicit null performs a lookup and reports Conversation owner task null does not exist. Both failed0.050s in the initial factory. The implementation now captures property presence before minting, retains the undefined/null distinction and uses the source error text. An independent original probe confirms these cases, staged ownership, nil/missing ownership-property errors, unknown ownership kinds and ignored function-valued option fields (complete original table12 plus80 supplements92 pass, zero skipped).

Native source-derived controls check option-property errors before allocation, the first-table-write read fence, ownerless record shape and ignored options. A real Harness control exercises initialization and an initializer's public entry append. These supplements are distinct from translated original tests.

Ownerless construction performs no committed read and does not drain an unrelated writer flight. A signal-held first entry write admits two blind public constructor callbacks and stores them in [1,2] calls. Ten shuffled constructor repetitions pass1.951s and ten blind-admission repetitions pass0.406s. The corresponding original-source probe passes in selected273, zero skipped. A later no-op admission callback provides a signal barrier proving both preparations finished before storage release. This test-only tightening follows the full run; the final ten repetitions0.413s and static gates pass again with unchanged production code.

An AST pass renames81 old one-argument raw Tx calls in45 files without changing Harness's two-argument method or new method-value probes. A text audit verifies42 files contain only those identifier changes; three existing semantic files and the new factory are reviewed separately. Existing native assertions remain, but private raw fixtures are not canonical constructor acceptance.

## Verification and remaining scope

Full repository ./... passes26 packages: root0.871s, durable525.645s/tools28.155s and public consumers12.505s. Nine compiled semantic faults, ten constructor repetitions1.951s, final ten blind-admission repetitions0.413s, vet/Staticcheck/logging/register pass. Selected original273 (200 unchanged +73 probes), including complete original table12+81 supplements93, pass with zero skipped. Analysed temporary logs/overlays are removed. Publication-record identity, ForkConversation/SetTask and atomic owner packets, untyped/schema timing, complete builtin/API/Storage and pre-release acceptance remain open.

Faults independently corrupt record return, ID allocation, owner conversation, staged-owner lookup, undefined-owner handling, null-owner text, write fencing, initialization and disjoint admission. An initial owner-conversation mutation left a variable unused and failed compilation; it received no semantic-failure credit. Correcting that mutation produced all nine expected compiled behavioural failures.

Go copied records still do not implement the source publication-object identity assertions. Canonical ForkConversation options, SetTask and the remaining atomic ownership packet, full untyped/schema/error timing, complete builtin producer migration, wider API/Storage and pre-release profiling remain open. This is not full constructor or release acceptance.
