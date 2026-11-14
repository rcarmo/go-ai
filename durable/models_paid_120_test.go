package durable

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"sync/atomic"
	"testing"

	goai "github.com/rcarmo/go-ai"
	"github.com/rcarmo/go-ai/images"
)

func TestModels120PaidValidationAndReservedAuthority(t *testing.T) {
	if e := validatePaidClassifierContext(goai.ClassifierContext{State: map[string]any{}, Questions: map[string]goai.ClassifierQuestion{}}); e == nil {
		t.Fatal("empty questions accepted")
	}
	if e := validatePaidImageContext(images.ImagesContext{}); e == nil {
		t.Fatal("empty image input accepted")
	}
	if e := validatePaidImageContext(images.ImagesContext{Input: []images.ImageInput{{Type: "image", Data: "x"}}}); e == nil {
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
