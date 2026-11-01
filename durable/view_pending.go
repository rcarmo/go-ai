package durable

// coalesceConversationFrames runs only for contiguous, successful exact-native
// projections, off the Session line. Adapters, raw subscriptions and projection
// errors retain their individual delivery boundary. Values and operations have
// independent DTO budgets, as in preparePublication.
func coalesceConversationFrames(frames []PublicationFrame, before ConversationView, limits Limits, quota int64) (PublicationFrame, error) {
	frame := frames[len(frames)-1]
	if len(frames) == 1 {
		return frame, nil
	}
	batches := make([][]Operation, len(frames))
	for i, pending := range frames {
		batches[i] = pending.conversation.ops
	}
	ops, overflow, err := foldWatchOperations(batches, limits)
	if err != nil {
		return PublicationFrame{}, err
	}
	value, err := dtoObject(frame.conversation.value, limits)
	if err != nil {
		return PublicationFrame{}, err
	}
	encodedValue, err := encodeBounded(value, limits, limits.MaxFramePayloadBytes)
	if err != nil {
		return PublicationFrame{}, err
	}
	if !overflow {
		ops, err = ownOperations(ops, limits)
		if err == nil {
			baseline, copyErr := dtoObject(before, limits)
			if copyErr != nil {
				return PublicationFrame{}, copyErr
			}
			_, err = ApplyOperations(baseline, ops, limits)
		}
		if err == nil {
			var encoded []byte
			encoded, err = encodeBounded(ops, limits, limits.MaxFramePayloadBytes)
			if err == nil && int64(len(encodedValue)+len(encoded)) <= quota {
				frame.conversation = &conversationPublicationFrame{value: frame.conversation.value, ops: ops}
				return frame, nil
			}
		}
	}
	ops, err = ownOperations([]Operation{{"r", value}}, limits)
	if err != nil {
		return PublicationFrame{}, err
	}
	encoded, err := encodeBounded(ops, limits, limits.MaxFramePayloadBytes)
	if err != nil {
		return PublicationFrame{}, err
	}
	if int64(len(encodedValue)+len(encoded)) > quota {
		return PublicationFrame{}, reject("conversation folded root exceeds process quota")
	}
	frame.conversation = &conversationPublicationFrame{value: frame.conversation.value, ops: ops}
	return frame, nil
}
