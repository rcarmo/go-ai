package durable

import (
	"strconv"
	"strings"
)

// Port of pinned v1.5.0 src/session/keys.ts: the keys of the Session line. A
// commit records the keys it writes; a job on the line touches the key it
// reads, so a read of an unpublished write waits for its publication.
//
// idText formats an ID the way storage JSON does, so a key derived from two
// equal IDs is byte-identical.
func idText(id ID) string { return strconv.FormatUint(uint64(id), 10) }

// taskKey is a task record.
func taskKey(id ID) string { return "task:" + idText(id) }

// conversationKey is a conversation record.
func conversationKey(id ID) string { return "conv:" + idText(id) }

// entryKey is an entry record.
func entryKey(id ID) string { return "entry:" + idText(id) }

// appendKey is the newest entries of a conversation: appended blind, read by
// scans, the newest entry, and head markers.
func appendKey(id ID) string { return "entry-append:" + idText(id) }

// submissionKey is a submission record.
func submissionKey(id ID) string { return "sub:" + idText(id) }

// requestKey is the submission a conversation's request ID names.
func requestKey(conversationID ID, requestID string) string {
	return "req:" + idText(conversationID) + ":" + requestID
}

// documentKey is a document address (addressID): its current incarnation,
// value, and held changes.
func documentKey(address DocumentAddress) string { return "doc:" + addressID(address) }

// incarnationKey is a document incarnation, for reads by its ID.
func incarnationKey(id ID) string { return "docid:" + idText(id) }

// scopeKey is the set of incarnations alive in a scope: created and retired
// blind, read by scans and the task-document index.
func scopeKey(scope string, owner ID) string {
	switch scope {
	case "session":
		return "docscope:session"
	case "conversation":
		return "docscope:c" + idText(owner)
	case "task":
		return "docscope:t" + idText(owner)
	}
	return ""
}

// ownedKey is a node of the scheduler's ownership tree (a task ID, or
// `c<conversation ID>`): the live work listed below it. Written by task
// creation and by a task becoming terminal for every node from the task's
// parent up to and including the nearest live task or the root conversation;
// read by every walk that expands the node.
func ownedKey(node string) string { return "owned:" + node }

// conversationNode is a conversation node of the ownership tree.
func conversationNode(id ID) string { return "c" + idText(id) }

const (
	// rootsKey is the ownerless conversations with live work below them, which
	// global idle walks from.
	rootsKey = "roots"
	// intentKey is the live tasks with cancellation intent: abort marks, and
	// held outcomes other than `completed`.
	intentKey = "intent"
	// allKeys is every unpublished write: what a read that cannot be keyed,
	// such as a scan, touches.
	allKeys = "*"
)

// addressID mirrors the reference encoding JSON.stringify([kind, scope.kind,
// owner, key ?? null]): a canonical string that is equal exactly when the
// address is equal. A session scope has no owner, and a singleton has no key;
// both encode as null so a keyed member with an empty key stays distinct.
func addressID(a DocumentAddress) string {
	parts := make([]string, 0, 4)
	parts = append(parts, strconv.Quote(a.Kind), strconv.Quote(a.Scope))
	if a.Scope == "session" {
		parts = append(parts, "null")
	} else {
		parts = append(parts, idText(a.Owner))
	}
	if a.Key == "" && !a.Family {
		parts = append(parts, "null")
	} else {
		parts = append(parts, strconv.Quote(a.Key))
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// tableKeys appends the keys of the table records in `writes`. Document writes
// are keyed by the transaction, which knows addresses, so they are skipped here.
func tableKeys(writes []Write, keys []string) []string {
	for _, write := range writes {
		switch {
		case write.Conversation != nil:
			keys = append(keys, conversationKey(write.Conversation.ID))
		case write.Entry != nil:
			keys = append(keys, entryKey(write.Entry.ID), appendKey(write.Entry.Conversation))
		case write.Task != nil:
			keys = append(keys, taskKey(write.Task.ID))
		case write.Submission != nil:
			keys = append(keys, submissionKey(write.Submission.ID))
			if write.Submission.RequestIDPresent {
				keys = append(keys, requestKey(write.Submission.Conversation, write.Submission.RequestID))
			}
		}
	}
	return keys
}
