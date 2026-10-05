package durable

import (
	"context"
	goai "github.com/rcarmo/go-ai"
)

type UsageState struct {
	Models map[string]goai.Usage `json:"models"`
	Tools  map[string]goai.Usage `json:"tools"`
}

func (h *Harness) Usage(ctx context.Context) (UsageState, error) { return h.readUsage(ctx, 0) }
func (c *ConversationHandle) Usage(ctx context.Context) (UsageState, error) {
	return c.h.readUsage(ctx, c.id)
}
func (h *Harness) readUsage(ctx context.Context, conversation ID) (UsageState, error) {
	aggregate := JSON{"models": map[string]any{}, "tools": map[string]any{}}
	err := h.session.readTasks(ctx, func(state Snapshot) error {
		if h.closing.Load() {
			return ErrClosed
		}
		if conversation != 0 {
			if _, ok := state.Conversations[conversation]; !ok {
				return reject("unknown conversation")
			}
		}
		for _, id := range ids(state.Documents) {
			doc := state.Documents[id]
			if doc.Kind != "pi.usage" || doc.Scope != "conversation" || doc.Retired || doc.Key != "" || doc.Family || conversation != 0 && doc.Owner != conversation {
				continue
			}
			for _, bucket := range []string{"models", "tools"} {
				values, ok := doc.Value[bucket].(map[string]any)
				if !ok {
					return reject("usage document shape")
				}
				for key, value := range values {
					raw, ok := value.(map[string]any)
					if !ok {
						return reject("usage counter shape")
					}
					var usage goai.Usage
					if err := fromObject(JSON(raw), &usage, h.session.limits); err != nil {
						return err
					}
					if !validUsage(&usage) {
						return reject("invalid usage counter")
					}
					if bucket == "tools" {
						if err := addToolUsage(aggregate, key, &usage, h.session.limits); err != nil {
							return err
						}
					} else {
						// addModelUsage's checked reducer is reused with an exact synthetic key;
						// move only the reduced value so provider/model slash keys remain intact.
						models := aggregate["models"].(map[string]any)
						temporary := map[string]any{}
						if prior, exists := models[key]; exists {
							temporary["/"+key] = prior
						}
						if err := addModelUsage(JSON{"models": temporary}, MessageReceipt{Model: key, Usage: &usage}, h.session.limits); err != nil {
							return err
						}
						models[key] = temporary["/"+key]
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return UsageState{}, err
	}
	var result UsageState
	err = fromObject(aggregate, &result, h.session.limits)
	return result, err
}
