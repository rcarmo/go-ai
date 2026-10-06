package durable

// Harness creation integration for raw Tx conversations and high-level forks.
// Plain Session transactions do not install harness documents.
func (h *Harness) initializeCreatedConversation(tx *Tx, conversation Conversation) (err error) {
	defer func() {
		if err != nil {
			tx.creationError = err
		}
	}()
	if conversation.Owner != 0 {
		candidate, err := tx.current()
		if err != nil {
			return err
		}
		owner, exists := candidate.Tasks[conversation.Owner]
		if !exists || !taskCanOwnNewWork(owner) {
			return reject("conversation owner missing, decided or abort-marked")
		}
	}
	if err := initializeBuiltins(tx, conversation.ID); err != nil {
		return err
	}
	candidate, err := tx.current()
	if err != nil {
		return err
	}
	if _, exists := agentDocument(candidate, conversation.ID); !exists {
		value := JSON{}
		if conversation.Parent == 0 && conversation.Owner != 0 {
			owner, exists := candidate.Tasks[conversation.Owner]
			if !exists {
				return reject("conversation owner missing")
			}
			if source, ok := agentDocument(candidate, owner.Conversation); ok {
				value, err = copyObject(source.Value, tx.limits)
				if err != nil {
					return err
				}
			}
		}
		id, err := tx.MintID()
		if err != nil {
			return err
		}
		if _, err := tx.CreateDocument(Document{ID: id, Scope: "conversation", Owner: conversation.ID, Kind: "pi.agent", Version: 1, History: "rewindable", Fork: "asOf", Value: value}); err != nil {
			return err
		}
	}
	if h.options.ConversationCreated != nil {
		return callConversationCreated(h.options.ConversationCreated, tx, conversation)
	}
	return nil
}
func callConversationCreated(callback func(*Tx, Conversation) error, tx *Tx, conversation Conversation) (err error) {
	defer func() {
		if recover() != nil {
			err = reject("conversation creation callback panic")
		}
	}()
	return callback(tx, conversation)
}
