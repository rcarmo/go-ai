package durable

import (
	"context"
	"encoding/json"
	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
	"math"
	"os"
	"strings"
	"sync/atomic"
	"testing"
)

func TestModels120PaidValidationAndReservedAuthority(t *testing.T) {
	if e := validatePaidClassifierContext(goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{}}); e == nil {
		t.Fatal("empty questions accepted")
	}
	if e := validatePaidImageContext(images.ImagesContext{}); e == nil {
		t.Fatal("empty image input accepted")
	}
	if e := validatePaidImageInput(map[string]any{"input": []any{map[string]any{"type": "image", "data": "x"}}}); e == nil {
		t.Fatal("missing MIME accepted")
	}
	registry := NewRegistry()
	reg := wrapRegistration(classifierToolName)
	if e := registry.Register(reg); e == nil {
		t.Fatal("reserved adapter forgery")
	}
	if e := installPaidModelTools(registry); e != nil {
		t.Fatal(e)
	}
	if e := installPaidModelTools(registry); e != nil {
		t.Fatal("reopen registration", e)
	}
	data, err := os.ReadFile("testdata/pi-durable-v150/model-tools.json")
	if err != nil {
		t.Fatal(err)
	}
	var original struct {
		Tools []JSON `json:"tools"`
	}
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	for _, want := range original.Tools {
		name := want["name"].(string)
		tool, found := registry.current(name)
		if !found {
			t.Error("original model-tool name missing", name)
			continue
		}
		got, err := dtoObject(JSON{"name": tool.offer.Name, "description": tool.offer.Description, "parameters": tool.offer.Schema, "structuredOutputSchema": tool.offer.StructuredOutputSchema, "callers": tool.offer.Callers}, DefaultLimits())
		if err != nil || !equalJSONValue(got, want) {
			t.Error("model-tool registration differs from original", got, err)
		}
	}
	for _, name := range []string{classifierToolName, imagesToolName} {
		tool, ok := registry.current(name)
		if !ok || tool.offer.ReplaySafe || toolCallerAllowed(tool.offer, "model") || !toolCallerAllowed(tool.offer, "tools") {
			t.Fatal(name, tool.offer)
		}
		if _, err := validateArguments(bg, tool, JSON{"model": nil, "context": nil}, DefaultLimits()); err != nil {
			t.Error("source unknown schema must admit null before tool validation", err)
		}
		for _, invalid := range []any{math.Inf(1), func() {}} {
			if _, err := validateArguments(bg, tool, JSON{"model": invalid, "context": nil}, DefaultLimits()); err == nil {
				t.Error("unknown schema widened bounded JSON authority")
			}
		}
	}
}

func TestModels120PaidTypedCallsDurableScopedAuthUsageAndKeyReuse(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		var classifierRuns, imageRuns, optionsRuns atomic.Int64
		goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: "paid-test-classifier", Classify: func(model *goai.ClassifierModel, input goai.ClassifierContext, opts *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
			classifierRuns.Add(1)
			if opts.Identity == nil || opts.Identity.Attribution == nil || opts.Identity.Attribution.Name != "pi" {
				t.Error("missing classifier harness attribution")
			}
			if opts.APIKey != "host-key" || opts.Context == nil || model.BaseURL != "" || model.Headers != nil {
				t.Error("classifier boundary", model, opts)
			}
			return &goai.ClassifierResult{Answers: map[string]goai.ClassifierAnswer{"q": {Type: "bool", Probability: 1}}, StopReason: "stop", Usage: &goai.Usage{Input: 2, Output: 1, TotalTokens: 3}}, nil
		}})

		images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: "paid-test-images", GenerateImages: func(model *images.ImagesModel, input images.ImagesContext, opts *images.ImagesOptions) (*images.AssistantImages, error) {
			imageRuns.Add(1)
			if opts.Identity == nil || opts.Identity.Attribution == nil || opts.Identity.Attribution.Name != "pi" {
				t.Error("missing image harness attribution")
			}
			if opts.APIKey != "image-key" || opts.Context == nil || model.BaseURL != "" || model.Headers != nil || len(input.Input) != 1 {
				t.Error("image boundary", model, opts)
			}
			return &images.AssistantImages{Output: []images.ImageOutput{{Type: "image", Data: "aGVsbG8=", MimeType: "image/png"}}, StopReason: "stop", Usage: &goai.Usage{Input: 4, Output: 2, TotalTokens: 6}}, nil
		}})
		defer images.UnregisterImagesApiProvider("paid-test-images")
		catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), image: &goai.ImageModel{Provider: "scoped", ID: "image", Api: "paid-test-images", Headers: map[string]string{"Authorization": "private"}, BaseURL: "private"}, classifier: &goai.ClassifierModel{Provider: "scoped", ID: "classifier", Api: "paid-test-classifier", Headers: map[string]string{"Authorization": "private"}, BaseURL: "private"}}
		registry := NewRegistry()
		def := taskDefinition(t, "paid-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			input := goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"q": {Type: "bool", Instructions: "yes?", Criteria: goai.ClassifierBoolCriteria{True: "yes", False: "no"}}}}
			for i := 0; i < 2; i++ {
				result, e := access.Classify(ctx, "classify", goai.TypedModelRef{Provider: "scoped", ID: "classifier"}, input)
				if e != nil {
					return e
				}
				if result.Answers["q"].Probability != 1 {
					t.Fatal(result)
				}
			}
			result, e := access.GenerateImages(ctx, "images", goai.TypedModelRef{Provider: "scoped", ID: "image"}, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}})
			if e != nil {
				return e
			}
			if len(result.Output) != 1 || result.Output[0].Data != "aGVsbG8=" {
				t.Fatal(result)
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Catalog: catalog, PaidModels: true, ClassifierRequestOptions: func(ctx context.Context, ref goai.TypedModelRef) (*goai.ClassifierOptions, error) {
			optionsRuns.Add(1)
			if ref.Type != goai.ModelTypeClassifier {
				t.Error(ref)
			}
			return &goai.ClassifierOptions{APIKey: "host-key"}, nil
		}, ImageRequestOptions: func(context.Context, goai.TypedModelRef) (*images.ImagesOptions, error) {
			optionsRuns.Add(1)
			return &images.ImagesOptions{APIKey: "image-key"}, nil
		}})
		root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
		if classifierRuns.Load() != 1 || imageRuns.Load() != 1 || optionsRuns.Load() != 2 {
			t.Fatal("paid key duplicated dispatch", classifierRuns.Load(), imageRuns.Load(), optionsRuns.Load())
		}
		usage, e := h.Usage(bg)
		if e != nil {
			t.Fatal(e)
		}
		if usage.Tools[classifierToolName].TotalTokens != 3 || usage.Tools[imagesToolName].TotalTokens != 6 {
			t.Fatal("paid usage missing", usage)
		}
		if e = h.Close(bg); e != nil {
			t.Fatal(e)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		if _, e = next.Snapshot(bg); e != nil {
			t.Fatal(e)
		}
		if classifierRuns.Load() != 1 || imageRuns.Load() != 1 {
			t.Fatal("open repeated paid effects")
		}
	})
}

func TestPaidModelResultOptionalErrorReplay(t *testing.T) {
	limits := DefaultLimits()
	for _, target := range []any{new(goai.ClassifierResult), new(images.AssistantImages)} {
		if err := decodeStrict([]byte(`{"unexpected":true}`), limits, limits.MaxRecordBytes, target); err == nil {
			t.Error("result codec bypassed strict field validation")
		}
	}
	for _, test := range []struct{ name, field, wire string }{
		{"absent", "", ""}, {"empty", `,"errorMessage":""`, `""`}, {"nonempty", `,"errorMessage":"failed"`, `"failed"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var classified goai.ClassifierResult
			if err := json.Unmarshal([]byte(`{"api":"presence-classifier","provider":"presence","model":"judge","answers":{},"stopReason":"error","timestamp":0`+test.field+`}`), &classified); err != nil {
				t.Fatal(err)
			}
			var generated images.AssistantImages
			if err := json.Unmarshal([]byte(`{"api":"presence-images","provider":"presence","model":"painter","output":[],"stopReason":"error","timestamp":0`+test.field+`}`), &generated); err != nil {
				t.Fatal(err)
			}
			for _, result := range []any{classified, generated} {
				bounded, err := dtoObject(result, DefaultLimits())
				if err != nil {
					t.Fatal(err)
				}
				_, present := bounded["errorMessage"]
				if present != (test.wire != "") {
					t.Error("bounded result lost error presence", bounded)
				}
			}
			var classCalls, imageCalls atomic.Int64
			goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: "presence-classifier", Classify: func(*goai.ClassifierModel, goai.ClassifierContext, *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
				classCalls.Add(1)
				return &classified, nil
			}})
			images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: "presence-images", GenerateImages: func(*images.ImagesModel, images.ImagesContext, *images.ImagesOptions) (*images.AssistantImages, error) {
				imageCalls.Add(1)
				return &generated, nil
			}})
			t.Cleanup(func() { images.UnregisterImagesApiProvider("presence-images") })
			check := func(result any) {
				wire, err := json.Marshal(result)
				if err != nil {
					t.Error(err)
					return
				}
				var fields map[string]json.RawMessage
				if err := json.Unmarshal(wire, &fields); err != nil {
					t.Error(err)
					return
				}
				if string(fields["errorMessage"]) != test.wire {
					t.Error("paid error presence changed", string(wire))
				}
				if _, present := fields["usage"]; present {
					t.Error("paid absent usage became present", string(wire))
				}
			}
			registry := NewRegistry()
			definition := taskDefinition(t, "paid-error-presence", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				access, err := r.Models()
				if err != nil {
					return err
				}
				for i := 0; i < 2; i++ {
					result, err := access.Classify(ctx, "judge", goai.TypedModelRef{Provider: "presence", ID: "judge"}, goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"q": {Type: "bool", Instructions: "yes?", Criteria: goai.ClassifierBoolCriteria{True: "yes", False: "no"}}}})
					if err != nil {
						return err
					}
					check(result)
					image, err := access.GenerateImages(ctx, "painter", goai.TypedModelRef{Provider: "presence", ID: "painter"}, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}})
					if err != nil {
						return err
					}
					check(image)
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			if _, err := registry.RegisterTask(definition); err != nil {
				t.Fatal(err)
			}
			catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), classifier: &goai.ClassifierModel{Provider: "presence", ID: "judge", Api: "presence-classifier"}, image: &goai.ImageModel{Provider: "presence", ID: "painter", Api: "presence-images"}}
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, Options{Registry: registry, Catalog: catalog, PaidModels: true})
			root(t, h, ModelRef{Provider: "presence", ID: "chat"})
			record := waitPublicTask(t, h, createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
			if record.State.Outcome.Status != "completed" || classCalls.Load() != 1 || imageCalls.Load() != 1 {
				t.Fatal("paid optional replay failed", record.State.Outcome, classCalls.Load(), imageCalls.Load())
			}
		})
	}
}

// Actual canonical internal-tool calls, with source argument envelopes and
// exact modelResult text. No direct Execute/helper substitutes the task path.
func TestSourceModelToolCallAndResultText(t *testing.T) {
	for _, test := range []struct{ name, stop, field, suffix string }{
		{"stop", "stop", "", "done"}, {"absent-error", "error", "", "error: "},
		{"empty-error", "error", `,"errorMessage":""`, "error: "}, {"nonempty-error", "error", `,"errorMessage":"failed"`, "error: failed"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var classified goai.ClassifierResult
			if err := json.Unmarshal([]byte(`{"api":"source-tool-classifier","provider":"presence","model":"judge","answers":{},"stopReason":"`+test.stop+`","timestamp":0`+test.field+`}`), &classified); err != nil {
				t.Fatal(err)
			}
			var generated images.AssistantImages
			if err := json.Unmarshal([]byte(`{"api":"source-tool-images","provider":"presence","model":"painter","output":[],"stopReason":"`+test.stop+`","timestamp":0`+test.field+`}`), &generated); err != nil {
				t.Fatal(err)
			}
			var classCalls, imageCalls atomic.Int64
			goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: "source-tool-classifier", Classify: func(*goai.ClassifierModel, goai.ClassifierContext, *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
				classCalls.Add(1)
				return &classified, nil
			}})
			images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: "source-tool-images", GenerateImages: func(*images.ImagesModel, images.ImagesContext, *images.ImagesOptions) (*images.AssistantImages, error) {
				imageCalls.Add(1)
				return &generated, nil
			}})
			t.Cleanup(func() { images.UnregisterImagesApiProvider("source-tool-images") })
			registry := NewRegistry()
			definition := taskDefinition(t, "source-model-tools", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
				for _, call := range []struct {
					name, id, key, verb string
					input               JSON
				}{
					{"pi.codemode.models.classify", "judge", "judge", "classify", JSON{"state": JSON{}, "questions": JSON{"q": JSON{"type": "bool", "instructions": "yes?", "criteria": JSON{"true": "yes", "false": "no"}}}}},
					{"pi.codemode.models.generateImages", "painter", "painter", "generateImages", JSON{"input": []any{JSON{"type": "text", "text": "draw"}}}},
				} {
					for i := 0; i < 2; i++ {
						result, err := r.CallTool(ctx, call.key, call.name, JSON{"model": JSON{"provider": "presence", "id": call.id}, "context": call.input})
						if err != nil {
							return err
						}
						want := call.verb + " presence/" + call.id + " " + test.suffix
						if result.Content != want || result.IsError != (test.stop != "stop") || !result.HasStructuredOutput {
							t.Error("source model-tool result changed", result.Content, result.IsError, result.HasStructuredOutput, "want", want)
						}
					}
				}
				return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
			})
			if _, err := registry.RegisterTask(definition); err != nil {
				t.Fatal(err)
			}
			catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), classifier: &goai.ClassifierModel{Provider: "presence", ID: "judge", Api: "source-tool-classifier"}, image: &goai.ImageModel{Provider: "presence", ID: "painter", Api: "source-tool-images"}}
			store, err := newTestSQLite(t)
			if err != nil {
				t.Fatal(err)
			}
			h := openHarness(t, store, Options{Registry: registry, Catalog: catalog, PaidModels: true})
			root(t, h, ModelRef{Provider: "presence", ID: "chat"})
			record := waitPublicTask(t, h, createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
			if record.State.Outcome.Status != "completed" || classCalls.Load() != 1 || imageCalls.Load() != 1 {
				t.Error("source model-tool execution changed", record.State.Outcome, classCalls.Load(), imageCalls.Load())
			}
		})
	}
}

// Source codemode/models.ts:123-169 accepts empty question IDs, instructions
// and criterion strings; score criteria need one level, not two.
func TestSourceClassifierContextEmptyStringsAndSingleScoreLevel(t *testing.T) {
	var effects atomic.Int64
	goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: "source-context-classifier", Classify: func(_ *goai.ClassifierModel, input goai.ClassifierContext, _ *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
		effects.Add(1)
		question, found := input.Questions[""]
		if !found || len(input.Questions) != 1 || question.Instructions != "" || input.State == nil {
			t.Error("source empty classifier strings changed before provider", input)
		}
		switch question.Type {
		case "choice":
			criteria, ok := question.Criteria.(map[string]string)
			if !ok || len(criteria) != 1 || criteria[""] != "" {
				t.Error("empty choice label/meaning changed", question.Criteria)
			}
		case "score":
			criteria, ok := question.Criteria.([]string)
			if !ok || len(criteria) != 1 || criteria[0] != "" {
				t.Error("single empty score level changed", question.Criteria)
			}
		case "bool":
			if criteria, ok := question.Criteria.(goai.ClassifierBoolCriteria); !ok || criteria.True != "" || criteria.False != "" {
				t.Error("empty bool meanings changed", question.Criteria)
			}
		default:
			t.Error("unexpected classifier question", question)
		}
		return &goai.ClassifierResult{Answers: map[string]goai.ClassifierAnswer{}, StopReason: "stop"}, nil
	}})
	registry := NewRegistry()
	definition := taskDefinition(t, "source.classifier-context", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		access, err := r.Models()
		if err != nil {
			return err
		}
		for _, question := range []goai.ClassifierQuestion{
			{Type: "choice", Criteria: map[string]string{"": ""}},
			{Type: "score", Criteria: []string{""}},
			{Type: "bool", Criteria: goai.ClassifierBoolCriteria{}},
		} {
			input := goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"": question}}
			encoded, err := json.Marshal(input)
			if err != nil {
				return err
			}
			var object JSON
			if err := json.Unmarshal(encoded, &object); err != nil {
				return err
			}
			produced, err := classifierContextObject(input, DefaultLimits())
			if err != nil || !equalJSONValue(produced, object) {
				t.Error("typed producer changed source classifier context", produced, object, err)
			}
			for repeat := 0; repeat < 2; repeat++ {
				result, err := r.CallTool(ctx, "raw-"+question.Type, classifierToolName, JSON{"model": JSON{"provider": "empty", "id": "judge"}, "context": object})
				if err != nil || result.IsError || !result.HasStructuredOutput {
					t.Error("source classifier context rejected through canonical tool", question.Type, result, err)
				}
				resultValue, err := access.Classify(ctx, "typed-"+question.Type, goai.TypedModelRef{Provider: "empty", ID: "judge"}, input)
				if err != nil || resultValue == nil {
					t.Error("source classifier context rejected through typed helper", question.Type, err)
				}
			}
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), classifier: &goai.ClassifierModel{Provider: "empty", ID: "judge", Api: "source-context-classifier"}}
	h := openHarness(t, store, Options{Registry: registry, Catalog: catalog, PaidModels: true})
	root(t, h, ModelRef{Provider: "empty", ID: "chat"})
	record := waitPublicTask(t, h, createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
	if record.State.Outcome.Status != "completed" || effects.Load() != 6 {
		t.Errorf("source classifier effect/replay changed: outcome=%+v error=%+v effects=%d", record.State.Outcome, record.State.Outcome.Error, effects.Load())
	}
	for _, invalid := range []goai.ClassifierQuestion{{Type: "choice", Criteria: map[string]string{}}, {Type: "score", Criteria: []string{}}, {Type: "bool", Criteria: map[string]string{"true": ""}}, {Type: "other"}} {
		if err := validatePaidClassifierContext(goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"": invalid}}); err == nil {
			t.Error("invalid classifier criteria admitted", invalid)
		}
	}
}

func TestModels120PaidImageRequestOptionsBoundary(t *testing.T) {
	for _, mode := range []string{"rewrite", "scope", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				const api images.ImagesApi = "paid-options-test"
				var runs atomic.Int64
				images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: api, GenerateImages: func(*images.ImagesModel, images.ImagesContext, *images.ImagesOptions) (*images.AssistantImages, error) {
					runs.Add(1)
					return &images.AssistantImages{StopReason: goai.StopReasonStop}, nil
				}})
				defer images.UnregisterImagesApiProvider(api)
				registry := NewRegistry()
				def := taskDefinition(t, "paid-options-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					access, e := r.Models()
					if e != nil {
						return e
					}
					ref := goai.TypedModelRef{Provider: "scoped", ID: "image"}
					input := images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}}
					if mode == "scope" {
						ref.Provider = "outside"
					}
					if mode == "invalid" {
						input.Input = nil
					}
					_, e = access.GenerateImages(ctx, "key", ref, input)
					if e == nil {
						return reject("paid boundary accepted invalid request")
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, e := registry.RegisterTask(def); e != nil {
					t.Fatal(e)
				}
				h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidImageCatalog120(api), ImageRequestOptions: func(context.Context, goai.TypedModelRef) (*images.ImagesOptions, error) {
					return &images.ImagesOptions{APIKey: "secret", OnPayload: func(payload map[string]any, _ *images.ImagesModel) (map[string]any, error) {
						payload["forged"] = true
						return payload, nil
					}}, nil
				}})
				root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
				record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
				if record.State.Outcome.Status != "completed" || runs.Load() != 0 {
					t.Fatalf("%+v %+v runs:%d", record.State.Outcome, record.State.Outcome.Error, runs.Load())
				}
				state, e := h.Snapshot(bg)
				if e != nil {
					t.Fatal(e)
				}
				for _, task := range state.Tasks {
					data, e := encodeBounded(task, h.session.limits, h.session.limits.MaxRecordBytes)
					if e != nil {
						t.Fatal(e)
					}
					if strings.Contains(string(data), "secret") {
						t.Fatal("request key persisted in task")
					}
				}
				for _, doc := range state.Documents {
					data, e := encodeBounded(doc.Value, h.session.limits, h.session.limits.MaxDocumentBytes)
					if e != nil {
						t.Fatal(e)
					}
					if strings.Contains(string(data), "secret") {
						t.Fatal("request key persisted in document")
					}
				}
			})
		})
	}
}

func paidImageCatalog120(api images.ImagesApi) *typedCatalog120 {
	return &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), image: &goai.ImageModel{Provider: "scoped", ID: "image", Api: goai.ImageApi(api)}, classifier: &goai.ClassifierModel{Provider: "scoped", ID: "classifier"}}
}
func TestModels120PaidImageUnsafeRestartDoesNotRedispatch(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		const api images.ImagesApi = "paid-restart-test"
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var runs, optionsCalls atomic.Int64
		images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: api, GenerateImages: func(_ *images.ImagesModel, _ images.ImagesContext, options *images.ImagesOptions) (*images.AssistantImages, error) {
			runs.Add(1)
			close(entered)
			<-options.Context.Done()
			close(cancelled)
			<-release
			return nil, options.Context.Err()
		}})
		defer images.UnregisterImagesApiProvider(api)
		registry := NewRegistry()
		var attempts atomic.Int64
		def := taskDefinition(t, "paid-restart-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			_, e = access.GenerateImages(ctx, "paid", goai.TypedModelRef{Provider: "scoped", ID: "image"}, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}})
			if attempts.Add(1) == 1 {
				return e
			}
			if e == nil {
				return reject("interrupted paid call unexpectedly produced structured output")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidImageCatalog120(api), ImageRequestOptions: func(context.Context, goai.TypedModelRef) (*images.ImagesOptions, error) {
			optionsCalls.Add(1)
			return &images.ImagesOptions{APIKey: "host"}, nil
		}})
		cleanupTaskGates(t, release)
		root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		awaitTaskSignal(t, entered)
		closed := make(chan error, 1)
		go func() { closed <- h.Close(bg) }()
		awaitTaskSignal(t, cancelled)
		select {
		case e := <-closed:
			t.Fatal("paid close skipped actual provider return", e)
		default:
		}
		releaseTaskGate(release)
		if e := watchReceive(t, closed); e != nil {
			t.Fatal(e)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || runs.Load() != 1 || optionsCalls.Load() != 1 {
			t.Fatalf("outcome:%+v error:%+v runs:%d options:%d", record.State.Outcome, record.State.Outcome.Error, runs.Load(), optionsCalls.Load())
		}
		state, e := next.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		found := false
		for _, doc := range state.Documents {
			if doc.Kind == nestedCallDocument {
				var call nestedCallState
				if e := decodeDocumentValue(doc, next.session.limits, &call); e != nil {
					t.Fatal(e)
				}
				if call.Name == imagesToolName {
					found = true
					if call.Result == nil || call.Result.Code != "interrupted" || call.Result.HasStructuredOutput {
						t.Fatal("unsafe restart result", call)
					}
				}
			}
		}
		if !found {
			t.Fatal("paid task/result missing")
		}
	})
}
func TestModels120PaidImageProviderErrorRetainsBillingOnce(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		const api images.ImagesApi = "paid-error-test"
		var runs atomic.Int64
		images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: api, GenerateImages: func(*images.ImagesModel, images.ImagesContext, *images.ImagesOptions) (*images.AssistantImages, error) {
			runs.Add(1)
			return &images.AssistantImages{StopReason: goai.StopReasonError, ErrorMessage: "provider rejected", Usage: &goai.Usage{Input: 5, Output: 1, TotalTokens: 6}}, nil
		}})
		defer images.UnregisterImagesApiProvider(api)
		registry := NewRegistry()
		def := taskDefinition(t, "paid-error-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			for i := 0; i < 2; i++ {
				result, e := access.GenerateImages(ctx, "paid", goai.TypedModelRef{Provider: "scoped", ID: "image"}, images.ImagesContext{Input: []images.ImageInput{{Type: "text", Text: "draw"}}})
				if e != nil {
					return e
				}
				if result.StopReason != goai.StopReasonError || result.ErrorMessage != "provider rejected" || result.Usage.TotalTokens != 6 {
					t.Fatal(result)
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidImageCatalog120(api)})
		root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
		record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
		usage, e := h.Usage(bg)
		if e != nil || usage.Tools[imagesToolName].TotalTokens != 6 || runs.Load() != 1 {
			t.Fatal("billed error lost/duplicated", usage, e, runs.Load())
		}
		if e = h.Close(bg); e != nil {
			t.Fatal(e)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		usage, e = next.Usage(bg)
		if e != nil || usage.Tools[imagesToolName].TotalTokens != 6 || runs.Load() != 1 {
			t.Fatal("reopen changed billing", usage, e, runs.Load())
		}
	})
}

func paidClassifierInput120() goai.ClassifierContext {
	return goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{"q": {Type: "bool", Instructions: "decide", Criteria: goai.ClassifierBoolCriteria{True: "yes", False: "no"}}}}
}
func paidClassifierCatalog120(api goai.ClassifierApi) *typedCatalog120 {
	return &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), image: &goai.ImageModel{Provider: "scoped", ID: "image"}, classifier: &goai.ClassifierModel{Provider: "scoped", ID: "classifier", Api: api}}
}
func TestModels120PaidClassifierUnsafeRestartAndProviderJoin(t *testing.T) {
	sqliteStore(t, func(t *testing.T, b sqliteFixture) {
		const api goai.ClassifierApi = "paid-classifier-restart"
		entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
		var calls, optionsCalls, attempts atomic.Int64
		goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: api, Classify: func(_ *goai.ClassifierModel, _ goai.ClassifierContext, options *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
			calls.Add(1)
			close(entered)
			<-options.Context.Done()
			close(cancelled)
			<-release
			return nil, options.Context.Err()
		}})
		registry := NewRegistry()
		def := taskDefinition(t, "classifier-restart-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			access, e := r.Models()
			if e != nil {
				return e
			}
			_, e = access.Classify(ctx, "paid", goai.TypedModelRef{Provider: "scoped", ID: "classifier"}, paidClassifierInput120())
			if attempts.Add(1) == 1 {
				return e
			}
			if e == nil {
				return reject("interrupted classifier returned structured result")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidClassifierCatalog120(api), ClassifierRequestOptions: func(context.Context, goai.TypedModelRef) (*goai.ClassifierOptions, error) {
			optionsCalls.Add(1)
			return &goai.ClassifierOptions{APIKey: "host-key"}, nil
		}})
		cleanupTaskGates(t, release)
		root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		awaitTaskSignal(t, entered)
		done := make(chan error, 1)
		go func() { done <- h.Close(bg) }()
		awaitTaskSignal(t, cancelled)
		select {
		case e := <-done:
			t.Fatal("classifier close skipped host join", e)
		default:
		}
		releaseTaskGate(release)
		if e := watchReceive(t, done); e != nil {
			t.Fatal(e)
		}
		next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
		record := waitPublicTask(t, next, id)
		if record.State.Outcome.Status != "completed" || calls.Load() != 1 || optionsCalls.Load() != 1 {
			t.Fatalf("%+v %+v calls:%d opts:%d", record.State.Outcome, record.State.Outcome.Error, calls.Load(), optionsCalls.Load())
		}
	})
}
func TestModels120PaidClassifierBilledErrorAndMalformedAnswer(t *testing.T) {
	for _, malformed := range []bool{false, true} {
		name := "error"
		if malformed {
			name = "malformed"
		}
		t.Run(name, func(t *testing.T) {
			sqliteStore(t, func(t *testing.T, b sqliteFixture) {
				const api goai.ClassifierApi = "paid-classifier-bill"
				var calls atomic.Int64
				goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: api, Classify: func(*goai.ClassifierModel, goai.ClassifierContext, *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
					calls.Add(1)
					answers := map[string]goai.ClassifierAnswer{}
					if malformed {
						answers["q"] = goai.ClassifierAnswer{Type: "unknown"}
					}
					return &goai.ClassifierResult{Answers: answers, StopReason: goai.StopReasonError, ErrorMessage: "classifier rejected", Usage: &goai.Usage{Input: 7, Output: 1, TotalTokens: 8}}, nil
				}})
				registry := NewRegistry()
				def := taskDefinition(t, "classifier-bill-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
					access, e := r.Models()
					if e != nil {
						return e
					}
					for i := 0; i < 2; i++ {
						result, e := access.Classify(ctx, "paid", goai.TypedModelRef{Provider: "scoped", ID: "classifier"}, paidClassifierInput120())
						if e != nil {
							return e
						}
						if result.StopReason != goai.StopReasonError || result.Usage == nil || result.Usage.TotalTokens != 8 {
							t.Fatal(result)
						}
					}
					return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
				})
				if _, e := registry.RegisterTask(def); e != nil {
					t.Fatal(e)
				}
				h := openHarness(t, b.store, Options{Registry: registry, PaidModels: true, Catalog: paidClassifierCatalog120(api)})
				root(t, h, ModelRef{Provider: "scoped", ID: "chat"})
				record := waitPublicTask(t, h, createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
				if record.State.Outcome.Status != "completed" {
					t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
				}
				usage, e := h.Usage(bg)
				if e != nil || usage.Tools[classifierToolName].TotalTokens != 8 || calls.Load() != 1 {
					t.Fatal("classifier billed error lost/duplicated", usage, e, calls.Load())
				}
				if e = h.Close(bg); e != nil {
					t.Fatal(e)
				}
				next := openHarness(t, reopenStoreAfterHarnessClose(t, b.store), h.options)
				usage, e = next.Usage(bg)
				if e != nil || usage.Tools[classifierToolName].TotalTokens != 8 || calls.Load() != 1 {
					t.Fatal("classifier reopen bill", usage, e, calls.Load())
				}
			})
		})
	}
}

// Source codemode/models.ts checkClassifierContext: absent images differ from
// an explicit array, while null/non-array/malformed image blocks reject.
func TestSourceClassifierImagesPresenceAndValidation(t *testing.T) {
	var effects atomic.Int64
	var wantImages []goai.ImageContent
	goai.RegisterClassifierApiProvider(&goai.ClassifierApiProvider{Api: "source-paid-images-presence", Classify: func(_ *goai.ClassifierModel, input goai.ClassifierContext, _ *goai.ClassifierOptions) (*goai.ClassifierResult, error) {
		effects.Add(1)
		if (input.Images == nil) != (wantImages == nil) || len(input.Images) != len(wantImages) {
			t.Error("paid classifier images presence changed", input.Images, wantImages)
		}
		for i, image := range input.Images {
			if i < len(wantImages) && image != wantImages[i] {
				t.Error("paid classifier image fields changed", image, wantImages[i])
			}
		}
		return &goai.ClassifierResult{Answers: map[string]goai.ClassifierAnswer{}, StopReason: "stop"}, nil
	}})
	registry := NewRegistry()
	definition := taskDefinition(t, "source.classifier-images", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		access, err := r.Models()
		if err != nil {
			return err
		}
		for i, images := range [][]goai.ImageContent{nil, {}, {{Type: "image", Data: "", MimeType: ""}}} {
			wantImages = images
			input := paidClassifierInput120()
			input.Images = images
			for repeat := 0; repeat < 2; repeat++ {
				result, err := access.Classify(ctx, strings.Repeat("valid", i+1), goai.TypedModelRef{Provider: "presence", ID: "judge"}, input)
				if err != nil || result == nil || result.StopReason != "stop" {
					t.Error("valid classifier image array rejected", images, result, err)
				}
			}
		}
		before := effects.Load()
		for i, invalid := range []any{nil, "not-array", []any{nil}, []any{JSON{"type": "text", "data": "", "mimeType": ""}}, []any{JSON{"type": "image", "data": ""}}, []any{JSON{"type": "image", "data": nil, "mimeType": ""}}} {
			result, err := r.CallTool(ctx, strings.Repeat("invalid", i+1), classifierToolName, JSON{"model": JSON{"provider": "presence", "id": "judge"}, "context": JSON{"state": JSON{}, "questions": JSON{"q": JSON{"type": "bool", "instructions": "", "criteria": JSON{"true": "", "false": ""}}}, "images": invalid}})
			if err != nil || !result.IsError {
				t.Error("source invalid classifier images admitted", invalid, result, err)
			}
		}
		if effects.Load() != before {
			t.Error("invalid classifier images reached provider", before, effects.Load())
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), classifier: &goai.ClassifierModel{Provider: "presence", ID: "judge", Api: "source-paid-images-presence", Input: []string{"text", "image"}}}
	h := openHarness(t, store, Options{Registry: registry, Catalog: catalog, PaidModels: true})
	root(t, h, ModelRef{Provider: "presence", ID: "chat"})
	record := waitPublicTask(t, h, createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
	if record.State.Outcome.Status != "completed" || effects.Load() != 3 {
		t.Error("classifier image validation/effects changed", record.State.Outcome, effects.Load())
	}
}

// Source checkImagesContext accepts empty strings but requires each selected
// block field to exist with string type before conversion to the typed DTO.
func TestSourceImageGenerationEmptyBlockStringsAndRequiredFields(t *testing.T) {
	for _, target := range []any{&images.ImageInput{}, &images.ImageOutput{}} {
		if err := decodeStrict([]byte(`{"type":"image","data":"","mimeType":"","unexpected":true}`), DefaultLimits(), 4096, target); err == nil {
			t.Error("SDK image bridge weakened unknown-field checks", target)
		}
	}
	var effects atomic.Int64
	images.RegisterImagesApiProvider(&images.ImagesApiProvider{Api: "source-empty-image-blocks", GenerateImages: func(_ *images.ImagesModel, input images.ImagesContext, _ *images.ImagesOptions) (*images.AssistantImages, error) {
		effects.Add(1)
		if len(input.Input) != 2 || input.Input[0] != (images.ImageInput{Type: "text"}) || input.Input[1] != (images.ImageInput{Type: "image"}) {
			t.Error("source image block fields changed before provider", input)
		}
		return &images.AssistantImages{Output: []images.ImageOutput{{Type: "text"}, {Type: "image"}}, StopReason: "stop"}, nil
	}})
	defer images.UnregisterImagesApiProvider("source-empty-image-blocks")
	registry := NewRegistry()
	definition := taskDefinition(t, "source.image-blocks", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
		access, err := r.Models()
		if err != nil {
			return err
		}
		input := images.ImagesContext{Input: []images.ImageInput{{Type: "text"}, {Type: "image"}}}
		want := JSON{"input": []any{JSON{"type": "text", "text": ""}, JSON{"type": "image", "data": "", "mimeType": ""}}}
		produced, err := dtoObject(input, DefaultLimits())
		if err != nil || !equalJSONValue(produced, want) {
			t.Error("typed image producer omitted required strings", produced, want, err)
		}
		for repeat := 0; repeat < 2; repeat++ {
			result, err := r.CallTool(ctx, "raw", imagesToolName, JSON{"model": JSON{"provider": "presence", "id": "painter"}, "context": want})
			if err != nil || result.IsError || !result.HasStructuredOutput {
				t.Error("source empty image strings rejected", result, err)
			} else {
				value, _ := result.StructuredOutput.(map[string]any)
				if !equalJSONValue(value["output"], want["input"]) {
					t.Error("image output union lost required empty fields", value["output"], want["input"])
				}
			}
			out, err := access.GenerateImages(ctx, "typed", goai.TypedModelRef{Provider: "presence", ID: "painter"}, input)
			if err != nil || out == nil || out.StopReason != "stop" || len(out.Output) != 2 {
				t.Error("typed empty image strings rejected", out, err)
			}
		}
		before := effects.Load()
		for i, invalid := range []any{nil, []any{}, "not-array", []any{nil}, []any{JSON{"type": "text"}}, []any{JSON{"type": "text", "text": nil}}, []any{JSON{"type": "image", "data": ""}}, []any{JSON{"type": "image", "data": nil, "mimeType": ""}}, []any{JSON{"type": "other", "text": ""}}} {
			result, err := r.CallTool(ctx, strings.Repeat("invalid", i+1), imagesToolName, JSON{"model": JSON{"provider": "presence", "id": "painter"}, "context": JSON{"input": invalid}})
			if err != nil || !result.IsError {
				t.Error("source invalid image input admitted", invalid, result, err)
			}
		}
		if effects.Load() != before {
			t.Error("invalid image input reached provider", before, effects.Load())
		}
		return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
	})
	if _, err := registry.RegisterTask(definition); err != nil {
		t.Fatal(err)
	}
	store, err := newTestSQLite(t)
	if err != nil {
		t.Fatal(err)
	}
	catalog := &typedCatalog120{ModelRuntime: goai.NewModelRuntime(nil), image: &goai.ImageModel{Provider: "presence", ID: "painter", Api: "source-empty-image-blocks", Input: []string{"text", "image"}}}
	h := openHarness(t, store, Options{Registry: registry, Catalog: catalog, PaidModels: true})
	root(t, h, ModelRef{Provider: "presence", ID: "chat"})
	record := waitPublicTask(t, h, createPublicTask(t, h, definition, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}}))
	if record.State.Outcome.Status != "completed" || effects.Load() != 2 {
		t.Error("source image input effects/replay changed", record.State.Outcome, effects.Load())
	}
}
