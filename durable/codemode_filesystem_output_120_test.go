package durable

import (
	"context"
	"sync"
	"testing"
)

// Embedding the unused interface makes accidental ambient filesystem calls fail.
// Only the explicit capability methods required by codemode are implemented.
type codemodeFS120 struct {
	FileSystem
	mu              sync.Mutex
	creates, writes int
	files           map[string][]byte
	id              string
}

func (f *codemodeFS120) Cwd() string { return "/owned" }
func (f *codemodeFS120) ID() string  { return f.id }
func (f *codemodeFS120) CreateTempDir(context.Context, string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.creates++
	return "/owned/tmp/codemode", nil
}
func (f *codemodeFS120) JoinPath(_ context.Context, parts ...string) (string, error) {
	return parts[0] + "/" + parts[1], nil
}
func (f *codemodeFS120) WriteFile(_ context.Context, path string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writes++
	if f.files == nil {
		f.files = map[string][]byte{}
	}
	f.files[path] = append([]byte(nil), data...)
	return nil
}
func TestCodemode120FilesystemDefaultWriterPersistsAddress(t *testing.T) {
	engine, err := NewQuickJSSandbox(bg)
	if err != nil {
		t.Fatal(err)
	}
	defer engine.Close(bg)
	backends(t, func(t *testing.T, b backend) {
		fs := &codemodeFS120{id: "owned-fs"}
		registry := NewRegistry()
		def := taskDefinition(t, "filesystem-output", func(ctx context.Context, _ TaskRecord, r *TaskRuntime) error {
			for i := 0; i < 2; i++ {
				result, err := r.RunCodemode(ctx, "script", "// @options: {\"max_output_tokens\":1}\ntext(\"long output\");", engine)
				if err != nil {
					return err
				}
				if result.FullOutputPath != "/owned/tmp/codemode/output.txt" {
					t.Error(result)
				}
			}
			return r.Commit(ctx, func(*Tx, TaskRecord) (*TaskState, error) { return taskDone(nil), nil })
		})
		if _, err = registry.RegisterTask(def); err != nil {
			t.Fatal(err)
		}
		h := openHarness(t, b.store, Options{Registry: registry, Env: func(context.Context, EnvTarget) (ExecutionEnvironment, error) { return fs, nil }})
		id := createPublicTask(t, h, def, nil, TaskOptions{Ownership: TaskOwnership{Kind: "conversation"}})
		record := waitPublicTask(t, h, id)
		if record.State.Outcome.Status != "completed" {
			t.Fatalf("%+v %+v", record.State.Outcome, record.State.Outcome.Error)
		}
		fs.mu.Lock()
		defer fs.mu.Unlock()
		if fs.creates != 1 || fs.writes != 1 || string(fs.files["/owned/tmp/codemode/output.txt"]) != "long output" {
			t.Fatal(fs.creates, fs.writes, fs.files)
		}
	})
}
