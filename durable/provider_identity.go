package durable

import (
	"context"

	goai "github.com/rcarmo/go-ai"
)

// A provider session belongs to one conversation. Initial fork policy ensures
// forks receive a new UUID rather than sharing prompt-cache/session affinity.
func ensureProviderDocument(tx *Tx, conversation ID) (string, error) {
	state, err := tx.current()
	if err != nil {
		return "", err
	}
	for _, document := range state.Documents {
		if document.Scope == "conversation" && document.Owner == conversation && document.Kind == "pi.provider" && !document.Retired {
			id, ok := document.Value["sessionId"].(string)
			if !ok || id == "" {
				return "", reject("invalid provider session identity")
			}
			return id, nil
		}
	}
	id := goai.UUIDv7()
	documentID, err := tx.mintID()
	if err != nil {
		return "", err
	}
	_, err = tx.createDocumentRecord(Document{ID: documentID, Scope: "conversation", Owner: conversation, Kind: "pi.provider", Version: 1, History: "latest", Fork: "initial", Value: JSON{"sessionId": id}})
	return id, err
}

func (r *TaskRuntime) providerSessionID(ctx context.Context) (string, error) {
	var id string
	err := r.harness.session.readTasks(ctx, func(state Snapshot) error {
		if err := r.check(); err != nil {
			return err
		}
		for _, document := range state.Documents {
			if document.Scope == "conversation" && document.Owner == r.conversation && document.Kind == "pi.provider" && !document.Retired {
				id, _ = document.Value["sessionId"].(string)
				if id == "" {
					return reject("invalid provider session identity")
				}
				break
			}
		}
		return nil
	})
	if err == nil && id == "" {
		err = r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
			var err error
			id, err = ensureProviderDocument(tx, r.conversation)
			return nil, err
		})
	}
	if err == nil && id != "" {
		r.harness.noteProviderSession(id)
	}
	return id, err
}

func (h *Harness) noteProviderSession(id string) {
	h.providerSessionsMu.Lock()
	defer h.providerSessionsMu.Unlock()
	if h.usedProviderSessions == nil {
		h.usedProviderSessions = make(map[string]struct{})
	}
	if _, known := h.usedProviderSessions[id]; !known {
		h.usedProviderSessions[id] = struct{}{}
		h.providerSessionOrder = append(h.providerSessionOrder, id)
	}
}

// Called after actual host joins. Creating or opening a conversation alone
// does not grant authority to close its provider resources.
func (h *Harness) cleanupProviderSessions() {
	h.providerSessionsMu.Lock()
	ids := h.providerSessionOrder
	h.providerSessionOrder, h.usedProviderSessions = nil, nil
	h.providerSessionsMu.Unlock()
	for _, id := range ids {
		if err := goai.CleanupSessionResources(id); err != nil {
			// A caller-owned reporter may reenter Close. Do not hold up shutdown
			// or replace a storage failure with a provider cleanup error.
			go h.report(context.Background(), err, ReportInfo{Type: "host_error", Callback: "models"})
		}
	}
}
