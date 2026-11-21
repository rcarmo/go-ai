package durable

import "encoding/json"

// SubmissionSettled delivers the same record returned by Submission.Wait.
var SubmissionSettled = submissionSettledToken()

func submissionSettledToken() *SignalToken {
	integer := JSON{"type": "integer"}
	text := JSON{"type": "string"}
	variant := func(kind, status string, fields []string) JSON {
		properties := JSON{"id": integer, "conversationId": integer, "type": JSON{"const": kind}, "status": JSON{"const": status}, "requestId": text,
			"notify": JSON{"type": "object", "properties": JSON{"taskId": integer, "key": text}, "required": []any{"taskId", "key"}, "additionalProperties": false}}
		required := []any{"id", "conversationId", "type", "status"}
		for _, field := range fields {
			properties[field] = integer
			required = append(required, field)
		}
		if status == "unanswered" {
			properties["reason"] = text
			required = append(required, "reason")
			if kind == "input" {
				properties["entry"] = integer
			}
		}
		return JSON{"type": "object", "properties": properties, "required": required}
	}
	token, err := DefineSignal(SignalDefinition{Name: "pi.submission-settled", Version: 1, Schema: JSON{"anyOf": []any{
		variant("input", "done", []string{"entry", "answer"}), variant("write", "done", []string{"entry"}), variant("continue", "done", []string{"answer"}),
		variant("input", "unanswered", nil), variant("write", "unanswered", nil), variant("continue", "unanswered", nil),
	}}})
	if err != nil {
		panic(err)
	}
	return token
}

func submissionNotification(raw any) *SubmissionNotify {
	obj := generationObject(raw)
	if obj == nil {
		return nil
	}
	key, _ := obj["key"].(string)
	return &SubmissionNotify{TaskID: submissionEntryID(obj["taskId"]), Key: key}
}

func (t *Tx) notifySubmission(record SubmissionRecord) error {
	notify := record.Notify
	candidate, found, err := t.candidateCanonicalTask(notify.TaskID)
	if err != nil || !found || candidate.State.Status == "terminal" || candidate.State.Status == "completing" || candidate.AbortRequested {
		return err
	}
	task, err := nativeTask(candidate, t.limits)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	var value any
	if err := decodePublicJSON(raw, &value); err != nil {
		return err
	}
	_, replacement, err := t.deliverSignal(task, SubmissionSettled, notify.Key, value)
	if err != nil || replacement == nil {
		return err
	}
	next, err := CanonicalTask(*replacement, t.limits)
	if err != nil {
		return err
	}
	return t.stageCanonicalTask(next)
}
