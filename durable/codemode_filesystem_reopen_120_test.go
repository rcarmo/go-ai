package durable

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCodemode120FilesystemRejectedResultReusesPersistedDirectory(t *testing.T) {
	for _, images := range []bool{false, true} {
		name := "text"
		if images {
			name = "images"
		}
		t.Run(name, func(t *testing.T) { testFilesystemRejectedResult120(t, images) })
	}
}
func testFilesystemRejectedResult120(t *testing.T, images bool) {
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, image.NewNRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	data := base64.StdEncoding.EncodeToString(encoded.Bytes())
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		registry := NewRegistry()
		writer := &codemodeFS120{id: "owned-fs"}
		code := "// @options: {\"max_output_tokens\":1}\nstore('valid',true);return 'long output';"
		if images {
			code = `store('valid',true);image({type:'image',mimeType:'image/png',data:'` + data + `'});image({type:'image',mimeType:'image/png',data:'` + data + `'});`
		}
		def := taskDefinition(t, "store-fault-parent", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			result, e := r.RunCodemode(ctx, "run", code, engine)
			if e == nil && images && (len(result.Images) != 2 || strings.Count(result.Output, "/owned/tmp/codemode/image-1.png") != 2) {
				t.Error("image paths/replay", result)
			}
			if e != nil {
				return e
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, e := registry.RegisterTask(def); e != nil {
			t.Fatal(e)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return writer, nil }})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		core := h.session.core
		original := core.appendFrame
		cause := reject("codemode store/result rejected")
		var failures atomic.Int64
		installTaskAppend(t, h, core, func(kind byte, ordinal, high uint64, payload []byte) error {
			if kind == 2 {
				var record commitRecord
				if e := decodeStrict(payload, h.session.limits, h.session.limits.MaxFramePayloadBytes, &record); e != nil {
					return e
				}
				for _, write := range record.Writes {
					var doc Document
					var value JSON
					if write.Document != nil {
						doc = *write.Document
						value = doc.Value
					} else if write.Delta != nil {
						doc = core.state.Documents[write.Delta.ID]
						changed, e := ApplyOperations(doc.Value, write.Delta.Ops, h.session.limits)
						if e != nil {
							return e
						}
						value = JSON(changed.(map[string]any))
					}
					if doc.Kind == codemodeRunKind && value["result"] != nil {
						failures.Add(1)
						return cause
					}
				}
			}
			if original != nil {
				return original(kind, ordinal, high, payload)
			}
			return nil
		})
		if e := h.Resume(bg); e != nil {
			t.Fatal(e)
		}
		ctx, cancel := context.WithTimeout(bg, 3*time.Second)
		defer cancel()
		end, e := h.session.Closed(ctx)
		if e != nil || !errors.Is(end.Error, cause) {
			t.Fatal(end, e)
		}
		if e = h.Close(ctx); e != nil {
			t.Fatal(e)
		}
		reopened := reopenStoreAfterHarnessClose(t, b.store)
		state, e := reopened.Snapshot(bg)
		if e != nil {
			t.Fatal(e)
		}
		for _, doc := range state.Documents {
			if doc.Kind == codemodeRunKind {
				if doc.Value["result"] != nil {
					t.Fatal("rejected result adopted")
				}
				if doc.Value["outputDir"] != "/owned/tmp/codemode" || doc.Value["outputFS"] != "owned-fs" {
					t.Fatal("output address not persisted", doc)
				}
			}
			if doc.Kind == "pi.codemode.store" && len(doc.Value["values"].(map[string]any)) != 0 {
				t.Fatal("store partially adopted", doc.Value)
			}
		}
		next := openHarness(t, reopened, h.options)
		record := waitPublicTask(t, next, id)
		writer.mu.Lock()
		defer writer.mu.Unlock()
		if record.State.Outcome.Status != "completed" || failures.Load() != 1 || writer.creates != 1 || writer.writes != 2 {
			t.Fatalf("outcome=%+v error=%+v dirs=%d writes=%d failures=%d", record.State.Outcome, record.State.Outcome.Error, writer.creates, writer.writes, failures.Load())
		}
		if images {
			decoded, err := png.Decode(bytes.NewReader(writer.files["/owned/tmp/codemode/image-1.png"]))
			if err != nil || decoded.Bounds().Dx() != 1 {
				t.Fatal("PNG recovery", err)
			}
			if len(writer.files) != 1 {
				t.Fatal("dedup files", writer.files)
			}
		} else if string(writer.files["/owned/tmp/codemode/output.txt"]) != "long output" {
			t.Fatal(writer.files)
		}
	})
}

func TestCodemode120FilesystemIdentityChangeRejectsBeforeWrite(t *testing.T) {
	backends(t, func(t *testing.T, b backend) {
		first := &codemodeFS120{id: "first"}
		other := &codemodeFS120{id: "other"}
		registry := NewRegistry()
		def := taskDefinition(t, "fs-identity", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			// A tiny fixture VM provides a spill result, then uses the persisted run address.
			_, err := r.RunCodemode(ctx, "script", "// @options: {\"max_output_tokens\":1}\nreturn 1", wallSandbox120{clock: new(atomic.Int64)})
			if err != nil {
				return err
			}
			if _, err = r.codemodeFilesystemWriter(ctx, "script", first); err != nil {
				return err
			}
			if _, err = r.codemodeFilesystemWriter(ctx, "script", other); err == nil {
				t.Error("filesystem identity change accepted")
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err := registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return first, nil }})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
		if other.creates != 0 || other.writes != 0 {
			t.Fatal(other.creates, other.writes)
		}
	})
}
