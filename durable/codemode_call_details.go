package durable

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Read the sealed journal and its nested results from the same owner-guarded
// view. Details do not admit calls or change a saved start, delivery or result.
func (r *TaskRuntime) codemodeCallDetails(ctx context.Context, key string) ([]any, error) {
	rows := []any{}
	if err := r.check(); err != nil {
		return nil, err
	}
	l := r.harness.session.limits
	err := r.harness.session.readTasks(ctx, func(state Snapshot) error {
		if err := r.check(); err != nil {
			return err
		}
		var journalDoc JSON
		settled := false
		nested := make(map[string]JSON)
		for _, doc := range state.Documents {
			if doc.Scope != "task" || doc.Owner != r.taskID || !doc.Family || doc.Retired {
				continue
			}
			switch doc.Kind {
			case codemodeRunKind:
				if doc.Key == key {
					settled = doc.Value["result"] != nil
				}
			case codemodeJournalKind:
				if doc.Key == key {
					journalDoc = doc.Value
				}
			case canonicalNestedCallDoc.options.Kind:
				if strings.HasPrefix(doc.Key, key+".c") {
					nested[doc.Key] = generationObject(doc.Value["result"])
				}
			}
		}
		if !settled || journalDoc == nil {
			return nil
		}
		var journal codemodeJournalState
		if err := fromObject(journalDoc, &journal, l); err != nil {
			return err
		}
		delivered := make(map[int]CodemodeDelivery, len(journal.Deliveries))
		for _, delivery := range journal.Deliveries {
			delivered[delivery.Call.ID] = delivery
		}
		// Reserve space for the output and the surrounding receipt. As in the
		// source, the arguments and errors are previews, not replay inputs.
		budget := min(l.MaxDocumentBytes, l.MaxRecordBytes) / 4
		for _, call := range journal.Issued {
			if call.Global {
				continue
			}
			args := "{}"
			if call.Arguments != nil {
				var buf bytes.Buffer
				encode := json.NewEncoder(&buf)
				encode.SetEscapeHTML(false)
				if err := encode.Encode(call.Arguments); err != nil {
					return err
				}
				args = strings.TrimSuffix(buf.String(), "\n")
			}
			row := JSON{"id": call.ID, "name": call.Name, "args": codemodeCallPreview(args, 200), "status": "cancelled"}
			if delivery, ok := delivered[call.ID]; ok {
				result := nested[fmt.Sprintf("%s.c%d", key, call.ID)]
				row["status"] = "ok"
				if delivery.Error != "" || result["isError"] == true {
					row["status"] = "error"
					message := delivery.Error
					if message == "" {
						message = codemodeNestedErrorText(result)
					}
					if message != "" {
						row["error"] = codemodeCallPreview(message, 500)
					}
				}
				if ms, ok := asNumber(result["durationMs"]); ok && ms >= 0 {
					row["durationMs"] = result["durationMs"]
				}
			}
			raw, err := encodeBounded(row, l, budget)
			if err != nil || len(raw)+1 > budget {
				break
			}
			rows = append(rows, row)
			budget -= len(raw) + 1
		}
		return r.check()
	})
	return rows, err
}

func codemodeCallPreview(text string, units int) string {
	if codemodeTextUnits(text) <= units {
		return text
	}
	return codemodePrefixUnits(text, units-3) + "..."
}

func codemodeNestedErrorText(result JSON) string {
	diagnostics, _ := result["diagnostics"].([]any)
	messages := []string{}
	for _, item := range diagnostics {
		diagnostic := generationObject(item)
		if text, ok := diagnostic["message"].(string); ok && diagnostic["severity"] == "error" {
			messages = append(messages, text)
		}
	}
	return strings.Join(messages, "\n")
}
