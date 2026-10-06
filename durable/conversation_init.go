package durable

import "context"

// ConversationInit runs on the creating transaction after builtin setup,
// ConversationCreated and the convenience agent replacement. It must use only Tx.
type ConversationInit func(*Tx, ID) error

func (h *Harness) CreateConversationWithInit(ctx context.Context, change AgentChange, init ConversationInit) (*ConversationHandle, error) {
	return h.createConversation(ctx, change, init)
}

func callConversationInit(init func(*Tx, ID) error, tx *Tx, id ID) (err error) {
	// Init has the same conversation-bound task/document defaults as Commit.
	previous := tx.taskConversation
	tx.taskConversation = id
	defer func() { tx.taskConversation = previous }()
	defer func() {
		if recover() != nil {
			err = reject("conversation init callback panic")
		}
	}()
	return init(tx, id)
}
