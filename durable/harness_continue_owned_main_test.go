package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	goai "github.com/rcarmo/go-ai"
)

// Only detached records and IDs cross the host callback boundary, never an
// invocation runtime, conversation, submission or ToolAPI capability.
type mainContinueOwnedObservation struct {
	owner, child, first, resumed ID
	placed, afterAbort           SubmissionRecord
	busy                         error
	disposition                  string
	err                          error
}

func mainContinueOwnedObserve(t *testing.T, observed <-chan mainContinueOwnedObservation) mainContinueOwnedObservation {
	t.Helper()
	select {
	case observation := <-observed:
		if observation.err != nil {
			t.Fatal("owned invocation:", observation.err)
		}
		return observation
	case <-time.After(3 * time.Second):
		t.Fatal("owned continue observation timed out")
		return mainContinueOwnedObservation{}
	}
}

func mainContinueOwnedUser(t *testing.T, state Snapshot, child, owner, first ID) {
	t.Helper()
	users := 0
	for _, entry := range state.Entries {
		if entry.Conversation != child || legacyEntryValue(entry)["role"] != "user" {
			continue
		}
		users++
		var message MessageReceipt
		if err := decodeEntryMessage(entry, DefaultLimits(), &message); err != nil {
			t.Fatal("decode child user receipt", err)
		}
		if entry.ID != first || entry.ByTask != owner || message.Role != goai.RoleUser || len(message.Content) != 1 || message.Content[0].Type != "text" || message.Content[0].Text != "work" {
			t.Fatalf("original child input lost its payload/caller attribution: %+v", entry)
		}
	}
	if users != 1 {
		t.Fatalf("child user entries = %d; want only the original input, no synthetic empty user", users)
	}
}

func mainContinueOwnedRequest(t *testing.T, messages []goai.Message) {
	t.Helper()
	nonSystem := 0
	for _, message := range messages {
		if message.Role == goai.RoleSystem {
			continue
		}
		nonSystem++
		if message.Role != goai.RoleUser || len(message.Content) != 1 || message.Content[0].Type != "text" || message.Content[0].Text != "work" {
			t.Errorf("owned continue provider context contains an invented/incorrect message: %+v", message)
		}
	}
	if nonSystem != 1 {
		t.Errorf("owned continue provider context has %d non-system messages; want original user only", nonSystem)
	}
}

func mainContinueOwnedFailure() *goai.ErrorEvent {
	message := terminal("").Message
	message.Content = nil
	message.StopReason, message.ErrorMessage = goai.StopReasonError, "Invalid request"
	return &goai.ErrorEvent{Reason: goai.StopReasonError, Error: message}
}

// Official v1.5.0 f9a26365568127461c47a07a37dcb42c140a3ac5,
// source-continue.test.ts:306-353, through a real registered delegate tool.
// Native adaptations: CreateOwnedConversation inherits the configured model;
// registered provider events and memory/journal replace Faux/SQLiteStorage.
// The tool owns the child, waits for its model_error, submits an entryless
// continue, observes busy rejection, waits for recovery and returns to the
// parent model. Attribution/context assertions below are derived additions.
func TestMainContinueOwnedToolAfterModelError(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		observed := make(chan mainContinueOwnedObservation, 1)
		busy, release := make(chan struct{}), make(chan struct{})
		registry := NewRegistry()
		if err := registry.Register(ToolRegistration{
			Definition:     goai.Tool{Name: "delegate", Parameters: json.RawMessage(`{"type":"object"}`)},
			Implementation: "continue.owned.delegate", Version: 1, ReplaySafe: true,
			Execute: func(ctx context.Context, _ JSON, api *ToolAPI) (result ToolResult, err error) {
				ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
				defer cancel()
				defer func() {
					if err != nil {
						select {
						case observed <- mainContinueOwnedObservation{err: err}:
						default:
						}
					}
				}()
				child, err := api.CreateOwnedConversation(ctx, "worker", nil)
				if err != nil {
					return ToolResult{}, err
				}
				first, err := child.Submit(ctx, Input{Content: "work"})
				if err != nil {
					return ToolResult{}, err
				}
				failed, err := first.Wait(ctx)
				if err != nil {
					return ToolResult{}, err
				}
				if failed.Record.Status != "unanswered" || failed.Record.Reason != "model_error" {
					return ToolResult{}, fmt.Errorf("first child settlement = %+v; want unanswered/model_error", failed.Record)
				}
				resumed, err := child.Submit(ctx, Input{Type: "continue", RequestID: "owned-resume"})
				if err != nil {
					return ToolResult{}, fmt.Errorf("bound continue Submit: %w", err)
				}
				placed, err := resumed.Status(ctx)
				if err != nil {
					return ToolResult{}, err
				}
				select {
				case <-busy:
				case <-ctx.Done():
					return ToolResult{}, ctx.Err()
				}
				_, busyErr := child.Submit(ctx, Input{Type: "continue"})
				observed <- mainContinueOwnedObservation{owner: api.TaskID(), child: child.ID(), first: first.ID(), resumed: resumed.ID(), placed: placed, busy: busyErr}
				settled, err := resumed.Wait(ctx)
				if err != nil {
					return ToolResult{}, err
				}
				if settled.Record.Type != "continue" || settled.Record.Status != "done" || settled.Record.Entry != 0 || settled.Record.Answer == 0 {
					return ToolResult{}, fmt.Errorf("child continue settlement = %+v", settled.Record)
				}
				return ToolResult{Content: settled.Record.Status}, nil
			},
		}); err != nil {
			t.Fatal(err)
		}
		var calls atomic.Int64
		ref, options := setupFakeAPI(t, func(ctx context.Context, _ *goai.Model, request *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
			ch := make(chan goai.Event, 1)
			switch n := calls.Add(1); n {
			case 1:
				ch <- toolAnswer("c1", "delegate", JSON{})
			case 2:
				mainContinueOwnedRequest(t, request.Messages)
				ch <- mainContinueOwnedFailure()
			case 3:
				mainContinueOwnedRequest(t, request.Messages)
				close(busy)
				go func() {
					defer close(ch)
					select {
					case <-release:
						ch <- terminal("child recovered")
					case <-ctx.Done():
					}
				}()
				return ch
			case 4:
				found := false
				for _, message := range request.Messages {
					if message.Role == goai.RoleToolResult && message.ToolCallID == "c1" && len(message.Content) == 1 && message.Content[0].Text == "done" {
						found = true
					}
				}
				if !found {
					t.Error("parent model did not receive delegate's recovered result", request.Messages)
				}
				ch <- terminal("done")
			default:
				t.Errorf("unexpected provider call %d", n)
			}
			close(ch)
			return ch
		})
		options.Registry = registry
		h := openHarness(t, b.store, options)
		cleanupTaskGates(t, release)
		parent := root(t, h, ref)
		sub, err := parent.Submit(bg, Input{Content: "go"})
		if err != nil {
			t.Fatal(err)
		}
		o := mainContinueOwnedObserve(t, observed)
		if !errors.Is(o.busy, ErrConversationBusy) || o.placed.Type != "continue" || o.placed.Status != "placed" || o.placed.Entry != 0 || o.placed.Answer != 0 || o.placed.Conversation != o.child {
			t.Fatal("bound continue admission/busy rejection", o)
		}
		state, err := h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		first := publicSubmission(state.Submissions[o.first])
		mainContinueOwnedUser(t, state, o.child, o.owner, first.Entry)
		run := generationObject(mainContinueOracleLive(t, state, o.child)["run"])
		generationID := submissionEntryID(run["task"])
		generation, owner := state.Tasks[generationID], state.Tasks[o.owner]
		if o.child == parent.ID() || state.Conversations[o.child].Owner != o.owner || owner.Kind != "pi.tool" || state.Tasks[owner.Owner].Conversation != parent.ID() || generationID == 0 || generationID == o.owner || generation.Kind != "pi.generation" || generation.Conversation != o.child || len(state.Submissions) != 3 || calls.Load() != 3 {
			t.Fatal("continue must belong to the tool-owned child's distinct run", o, run, generation, owner)
		}
		releaseTaskGate(release)
		if settled := waitSubmission(t, sub); settled.Record.Status != "done" || settled.Message == nil || len(settled.Message.Content) != 1 || settled.Message.Content[0].Text != "done" {
			t.Fatal("parent did not complete after delegate", settled)
		}
		resumed, err := h.Submission(bg, o.resumed)
		if err != nil || resumed == nil {
			t.Fatal("lookup continue", err)
		}
		settled := waitSubmission(t, resumed)
		state, err = h.Snapshot(bg)
		if err != nil {
			t.Fatal(err)
		}
		answer := state.Entries[settled.Record.Answer]
		var answerMessage MessageReceipt
		if err := decodeEntryMessage(answer, h.session.limits, &answerMessage); err != nil {
			t.Fatal("decode recovered answer", err)
		}
		if settled.Record.Type != "continue" || settled.Record.Status != "done" || settled.Record.Entry != 0 || settled.Task.ID != generationID || answer.ID == 0 || answer.Conversation != o.child || answer.ByTask != generationID || answerMessage.Role != goai.RoleAssistant || len(answerMessage.Content) != 1 || answerMessage.Content[0].Type != "text" || answerMessage.Content[0].Text != "child recovered" || state.Tasks[o.owner].Status != "done" || calls.Load() != 4 {
			t.Fatal("child recovery/answer attribution", settled, answer)
		}
		if old := publicSubmission(state.Submissions[o.first]); old.Status != "unanswered" || old.Reason != "model_error" || old.Entry != first.Entry {
			t.Fatal("continue changed failed input", old)
		}
		mainContinueOwnedUser(t, state, o.child, o.owner, first.Entry)
		if failure, exists := mainContinueOracleLive(t, state, o.child)["runFailure"]; exists {
			t.Fatal("recovered child retained runFailure", failure)
		}
	})
}

// Derived native regression, not another translated source declaration:
// continue is placed despite Entry=0, even before the provider is invoked.
// Submission.Abort must not withdraw it; conversation/task abort still ends
// its real generation normally. The owner is an actual registered native task.
func TestMainContinueOwnedSubmissionAbortBeforeProviderDerived(t *testing.T) {
	for _, mode := range []string{"conversation", "task"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				observed := make(chan mainContinueOwnedObservation, 1)
				beforeProvider := make(chan struct{})
				var hooks, calls atomic.Int64
				registry := NewRegistry()
				if err := registry.Install(&Extension{Name: "continue.owned.before-provider", Hooks: GenerationHooks{
					BeforeRequest: func(ctx context.Context, messages []MessageReceipt, _ *HookAPI) ([]MessageReceipt, error) {
						if hooks.Add(1) == 2 {
							close(beforeProvider)
							<-ctx.Done()
							return nil, ctx.Err()
						}
						return messages, nil
					},
				}}); err != nil {
					t.Fatal(err)
				}
				owner := taskDefinition(t, "task.continue.owned.abort", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) (err error) {
					ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
					defer cancel()
					defer func() {
						if err != nil {
							select {
							case observed <- mainContinueOwnedObservation{err: err}:
							default:
							}
						}
					}()
					child, err := r.CreateOwnedConversation(ctx, "worker", nil)
					if err != nil {
						return err
					}
					first, err := child.Submit(ctx, Input{Content: "work"})
					if err != nil {
						return err
					}
					failed, err := first.Wait(ctx)
					if err != nil {
						return err
					}
					if failed.Record.Status != "unanswered" || failed.Record.Reason != "model_error" {
						return fmt.Errorf("first child settlement = %+v", failed.Record)
					}
					resumed, err := child.Submit(ctx, Input{Type: "continue"})
					if err != nil {
						return fmt.Errorf("bound continue Submit: %w", err)
					}
					placed, err := resumed.Status(ctx)
					if err != nil {
						return err
					}
					select {
					case <-beforeProvider:
					case <-ctx.Done():
						return ctx.Err()
					}
					disposition, err := resumed.Abort(ctx)
					if err != nil {
						return err
					}
					after, err := resumed.Status(ctx)
					if err != nil {
						return err
					}
					observed <- mainContinueOwnedObservation{owner: r.TaskID(), child: child.ID(), first: first.ID(), resumed: resumed.ID(), placed: placed, afterAbort: after, disposition: disposition}
					settled, err := resumed.Wait(ctx)
					if err != nil {
						return err
					}
					if settled.Record.Type != "continue" || settled.Record.Status != "unanswered" || settled.Record.Reason != "aborted" || settled.Record.Entry != 0 || settled.Record.Answer != 0 {
						return fmt.Errorf("aborted child continue = %+v", settled.Record)
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone("observed abort"), nil })
				})
				if _, err := registry.RegisterTask(owner); err != nil {
					t.Fatal(err)
				}
				ref, options := setupFakeAPI(t, func(_ context.Context, _ *goai.Model, request *goai.Context, _ *goai.StreamOptions) <-chan goai.Event {
					if calls.Add(1) != 1 {
						t.Error("entryless continue reached provider despite pre-provider abort")
					}
					mainContinueOwnedRequest(t, request.Messages)
					ch := make(chan goai.Event, 1)
					ch <- mainContinueOwnedFailure()
					close(ch)
					return ch
				})
				options.Registry = registry
				h := openHarness(t, b.store, options)
				parent := root(t, h, ref)
				ownerID := createPublicTask(t, h, owner, nil, TaskOptions{Conversation: parent.ID(), Ownership: TaskOwnership{Kind: "conversation"}})
				if err := h.Resume(bg); err != nil {
					t.Fatal(err)
				}
				o := mainContinueOwnedObserve(t, observed)
				if o.owner != ownerID || o.disposition != "already_placed" || o.placed.Type != "continue" || o.placed.Status != "placed" || o.placed.Entry != 0 || o.placed.Answer != 0 || o.afterAbort.ID != o.resumed || o.afterAbort.Type != "continue" || o.afterAbort.Status != "placed" || o.afterAbort.Entry != 0 || o.afterAbort.Answer != 0 || calls.Load() != 1 {
					t.Fatal("Submission.Abort must leave the pre-provider continue placed", o, calls.Load())
				}
				state, err := h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				first := publicSubmission(state.Submissions[o.first])
				mainContinueOwnedUser(t, state, o.child, ownerID, first.Entry)
				generationID := submissionEntryID(generationObject(mainContinueOracleLive(t, state, o.child)["run"])["task"])
				if generationID == 0 || generationID == ownerID || state.Tasks[generationID].Conversation != o.child || state.Conversations[o.child].Owner != ownerID {
					t.Fatal("continue lost its child generation", generationID)
				}
				ctx, cancel := context.WithTimeout(bg, 3*time.Second)
				defer cancel()
				if mode == "conversation" {
					child, err := h.Conversation(ctx, o.child)
					if err != nil {
						t.Fatal(err)
					}
					if err := child.Abort(ctx); err != nil {
						t.Fatal("conversation abort", err)
					}
				} else if disposition, err := h.AbortTask(ctx, generationID); err != nil || disposition != "marked" {
					t.Fatal("generation task abort", disposition, err)
				}
				if result := waitPublicTask(t, h, generationID); result.State.Outcome == nil || result.State.Outcome.Status != "aborted" {
					t.Fatal("continue generation did not abort normally", result)
				}
				if result := waitPublicTask(t, h, ownerID); result.State.Outcome == nil || result.State.Outcome.Status != "completed" {
					t.Fatal("owner failed to observe child abort", result)
				}
				state, err = h.Snapshot(bg)
				if err != nil {
					t.Fatal(err)
				}
				final := publicSubmission(state.Submissions[o.resumed])
				if final.Type != "continue" || final.Status != "unanswered" || final.Reason != "aborted" || final.Entry != 0 || final.Answer != 0 || calls.Load() != 1 || hooks.Load() != 2 || len(generationObject(mainContinueOracleLive(t, state, o.child)["run"])) != 0 {
					t.Fatal("pre-provider continue abort settlement", final, calls.Load(), hooks.Load())
				}
				mainContinueOwnedUser(t, state, o.child, ownerID, first.Entry)
			})
		})
	}
}
