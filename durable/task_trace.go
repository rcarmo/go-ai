package durable

import goai "github.com/rcarmo/go-ai"

// The closed data union is checked before any encoder can observe caller data.
// In particular, arbitrary MarshalJSON methods, pointers and backend Values are
// not propagation carriers. Return an owned typed map for both Go and JSON input.
func copyTaskTrace(value any, l Limits) (any, error) {
	if value == nil {
		return nil, nil
	}
	if marker, ok := value.(string); ok {
		if marker == "root" {
			return marker, nil
		}
		return nil, reject("task trace root marker")
	}
	// Check aggregate/string/member budgets before allocating an owned copy.
	// Only plain maps reach the strict encoder below.
	switch source := value.(type) {
	case goai.TelemetryCarrier:
		if source == nil {
			return nil, reject("task trace must be an object")
		}
	case map[string]string:
		if source == nil {
			return nil, reject("task trace must be an object")
		}
	case map[string]any:
		if source == nil {
			return nil, reject("task trace must be an object")
		}
		for _, v := range source {
			if _, ok := v.(string); !ok {
				return nil, reject("task trace values must be strings")
			}
		}
	default:
		return nil, reject("task trace must be an object or root")
	}
	if _, err := encodeBounded(value, l, l.MaxRecordBytes); err != nil {
		return nil, err
	}
	out := goai.TelemetryCarrier{}
	switch source := value.(type) {
	case goai.TelemetryCarrier:
		if source == nil {
			return nil, reject("task trace must be an object")
		}
		for k, v := range source {
			out[k] = v
		}
	case map[string]string:
		if source == nil {
			return nil, reject("task trace must be an object")
		}
		for k, v := range source {
			out[k] = v
		}
	case map[string]any:
		if source == nil {
			return nil, reject("task trace must be an object")
		}
		for k, v := range source {
			str, ok := v.(string)
			if !ok {
				return nil, reject("task trace values must be strings")
			}
			out[k] = str
		}
	default:
		return nil, reject("task trace must be an object or root")
	}
	return out, nil
}
func copyTaskLinks(links *[]goai.TelemetryCarrier, l Limits) (*[]goai.TelemetryCarrier, error) {
	if links == nil {
		return nil, nil
	}
	if *links == nil {
		return nil, reject("task links must be an array")
	}
	if _, err := encodeBounded(links, l, l.MaxRecordBytes); err != nil {
		return nil, err
	}
	out := make([]goai.TelemetryCarrier, len(*links))
	for i, link := range *links {
		owned, err := copyTaskTrace(link, l)
		if err != nil {
			return nil, err
		}
		out[i] = owned.(goai.TelemetryCarrier)
	}
	return &out, nil
}

// Called only on the transaction line. Reading immutable Context snapshots is
// data copying, not backend injection/extraction; no host callback runs here.
func (tx *Tx) bindTaskTrace(task *Task, options TaskOptions) error {
	trace, links := options.Trace, options.Links
	if options.SubmissionIDs != nil {
		state, err := tx.current()
		if err != nil {
			return err
		}
		trace, links = nil, nil
		for i, id := range options.SubmissionIDs {
			submission, ok := state.Submissions[id]
			if created, found := tx.submissions[id]; found {
				// Creation records live in the transaction map until assembly;
				// this internal trace selection is not a caller table read.
				submission.Trace, ok = created.Trace, true
			}
			if !ok {
				return reject("submission not found for task trace")
			}
			if i == 0 {
				trace = "root"
				if submission.Trace != nil {
					trace = *submission.Trace
				}
			} else if submission.Trace != nil {
				if links == nil {
					items := []goai.TelemetryCarrier{}
					links = &items
				}
				*links = append(*links, *submission.Trace)
			}
		}
	}
	if trace == "root" {
		trace = nil
	} else if trace == nil {
		if parent := tx.taskTraceContext(); parent != nil && parent.Context != nil {
			trace = parent.Context
		}
	}
	owned, err := copyTaskTrace(trace, tx.limits)
	if err != nil {
		return err
	}
	ownedLinks, err := copyTaskLinks(links, tx.limits)
	if err != nil {
		return err
	}
	task.Trace, task.Links = owned, ownedLinks
	// Explicit root and a creator without propagation data cannot inherit a
	// process-local span accidentally. Adoption, not staging, applies this fence.
	if owned == nil {
		if tx.taskTraceRoots == nil {
			tx.taskTraceRoots = map[ID]bool{}
		}
		tx.taskTraceRoots[task.ID] = true
	} else {
		delete(tx.taskTraceRoots, task.ID)
	}
	return nil
}

// Runtime binding identifies the creating scope even when its native caller
// supplies Background rather than explicitly forwarding the phase context.
func (tx *Tx) taskTraceContext() *goai.TelemetrySpan {
	parent := TelemetryFromContext(tx.ctx)
	if parent == nil && tx.session != nil && tx.session.taskScheduler != nil {
		creator := tx.byTask
		if creator == 0 {
			creator = tx.progressTask
		}
		if runtime := tx.session.taskScheduler.invocations[creator]; runtime != nil && runtime.telemetry != nil {
			parent = runtime.telemetry.span.Load()
		}
	}
	return parent
}

func (tx *Tx) bindGenerationTrace(task *Task, cp generationCheckpoint, continuation bool) error {
	inputs := make([]ID, 0, len(cp.Steered)+1)
	if !continuation {
		inputs = append(inputs, cp.Submission)
	}
	inputs = append(inputs, cp.Steered...)
	if continuation {
		inputs = append(inputs, cp.Submission)
	}
	return tx.bindTaskTrace(task, TaskOptions{SubmissionIDs: inputs})
}
