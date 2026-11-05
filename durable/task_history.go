package durable

import "context"

// Entries reads committed fork-aware raw history bound to this invocation's
// conversation. Inclusive ID ranges survive compaction and crash/replay. A
// foreign Conversation in the native shared query type is rejected; zero binds
// it implicitly. Cursors remain opaque and bound to this exact query.
func (r *TaskRuntime) Entries(ctx context.Context, query EntryQuery, limit int, cursor Cursor) (Page[Entry], error) {
	if err := r.check(); err != nil {
		return Page[Entry]{}, err
	}
	if query.Conversation != 0 && query.Conversation != r.conversation {
		return Page[Entry]{}, reject("history query is outside invocation conversation")
	}
	query.Conversation = r.conversation
	var result Page[Entry]
	err := r.harness.session.ReadOnLine(ctx, func(line *ReadLine) error {
		if err := r.check(); err != nil {
			return err
		}
		var err error
		result, err = line.ScanEntries(query, limit, cursor)
		if err != nil {
			return err
		}
		return r.check()
	})
	if err != nil {
		return Page[Entry]{}, err
	}
	if err := r.check(); err != nil {
		return Page[Entry]{}, err
	}
	return result, nil
}

func (a *InvocationReader) Entries(ctx context.Context, query EntryQuery, limit int, cursor Cursor) (Page[Entry], error) {
	return a.runtime.Entries(ctx, query, limit, cursor)
}
