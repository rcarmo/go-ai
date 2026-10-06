package durable

import (
	"fmt"
	"strconv"
)

func appendPassiveWrite(tx *Tx, conversation ID, value JSON) error {
	id, err := tx.MintID()
	if err != nil {
		return err
	}
	draft := Entry{ID: id, Conversation: conversation, Kind: "message"}
	if raw, ok := value["entry"].(map[string]any); ok {
		if err := fromObject(JSON(raw), &draft, tx.limits); err != nil {
			return err
		}
		draft.ID, draft.Conversation, draft.Seq, draft.Position = id, conversation, 0, 0
	} else if message, ok := value["inputMessage"].(map[string]any); ok {
		draft.Value, err = copyObject(JSON(message), tx.limits)
		if err != nil {
			return err
		}
	} else {
		text, _ := value["content"].(string)
		draft.Value, err = dtoObject(userReceipt(text), tx.limits)
		if err != nil {
			return err
		}
	}
	if err := tx.AppendEntry(draft); err != nil {
		return err
	}
	value["placedEntry"] = id
	return nil
}
func (h *Harness) placeQueuedWrites(tx *Tx, conversation ID) error {
	inbox, err := builtin(tx, conversation, "pi.inbox")
	if err != nil {
		return err
	}
	value, err := inbox.Get()
	if err != nil {
		return err
	}
	items, ok := value["items"].([]any)
	if !ok {
		return reject("inbox shape")
	}
	keep := []any{}
	changed := false
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return reject("inbox item")
		}
		if object["mode"] != "write" {
			keep = append(keep, item)
			continue
		}
		number, err := strconv.ParseUint(fmt.Sprint(object["id"]), 10, 64)
		if err != nil {
			return err
		}
		sub, ok := tx.state.Submissions[ID(number)]
		if !ok || sub.Type != "write" {
			return reject("queued write missing")
		}
		if !terminalStatus(sub.Status) {
			state, err := tx.current()
			if err != nil {
				return err
			}
			stale, err := passiveHeadStale(state, conversation, sub.Value, tx.limits)
			if err != nil {
				return err
			}
			if stale {
				sub.Status = "failed"
				value, err := copyObject(sub.Value, tx.limits)
				if err != nil {
					return err
				}
				value["errorCode"] = "stale"
				sub.Value = value
			} else {
				sub.Value, err = copyObject(sub.Value, tx.limits)
				if err != nil {
					return err
				}
				if err := appendPassiveWrite(tx, conversation, sub.Value); err != nil {
					return err
				}
				sub.Status = "done"
			}
			if err := tx.PutSubmission(sub); err != nil {
				return err
			}
		}
		changed = true
	}
	if changed {
		value["items"] = keep
		return inbox.Set(value)
	}
	return nil
}

func admitCompactionWrite(tx *Tx, conversation, task ID, draft Entry) (ID, error) {
	request := fmt.Sprintf("compaction:%d", task)
	for _, sub := range tx.state.Submissions {
		if sub.Conversation == conversation && sub.RequestID == request {
			return sub.ID, nil
		}
	}
	wire, err := dtoObject(draft, tx.limits)
	if err != nil {
		return 0, err
	}
	id, err := tx.MintID()
	if err != nil {
		return 0, err
	}
	value := JSON{"entry": map[string]any(wire)}
	busy := false
	for _, work := range tx.state.Tasks {
		if work.Conversation == conversation && work.Kind == "pi.generation" && !terminalStatus(work.Status) {
			busy = true
			break
		}
	}
	status := "done"
	if busy {
		status = "pending"
	}
	sub := Submission{ID: id, Conversation: conversation, RequestID: request, Type: "write", Status: status, Value: value}
	if err = tx.PutSubmission(sub); err != nil {
		return 0, err
	}
	if busy {
		inbox, inboxErr := builtin(tx, conversation, "pi.inbox")
		if inboxErr != nil {
			return 0, inboxErr
		}
		err = inbox.Update(func(value JSON) error {
			items, ok := value["items"].([]any)
			if !ok {
				return reject("inbox shape")
			}
			value["items"] = append(items, JSON{"id": id, "mode": "write"})
			return nil
		})
	} else {
		state, e := tx.current()
		if e != nil {
			return 0, e
		}
		stale, e := passiveHeadStale(state, conversation, value, tx.limits)
		if e != nil {
			return 0, e
		}
		if stale {
			sub.Status = "failed"
			value["errorCode"] = "stale"
			err = tx.PutSubmission(sub)
		} else {
			err = appendPassiveWrite(tx, conversation, value)
			if err == nil {
				sub.Value = value
				err = tx.PutSubmission(sub)
			}
		}
	}
	return id, err
}

func passiveHeadStale(state Snapshot, conversation ID, value JSON, limits Limits) (bool, error) {
	raw, ok := value["entry"].(map[string]any)
	if !ok {
		return false, nil
	}
	var draft Entry
	if err := fromObject(JSON(raw), &draft, limits); err != nil {
		return false, err
	}
	if draft.Head == 0 {
		return false, nil
	}
	view, err := deriveContextView(state, conversation, 0, limits)
	if err != nil {
		return false, err
	}
	return view.Head != nil && draft.Head < view.Head.Head, nil
}
