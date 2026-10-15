package durable

import (
	"context"
	"strings"
)

// Filesystem output uses only the environment capability. The directory is
// persisted before writes so retries/reopen reuse fixed output filenames.
func (r *TaskRuntime) codemodeFilesystemWriter(ctx context.Context, key string, fs FileSystem) (writer CodemodeOutputWriter, err error) {
	defer func() {
		if recover() != nil {
			writer = nil
			err = reject("codemode filesystem output callback panic")
		}
	}()
	if ctx == nil {
		return nil, reject("nil codemode output context")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	id := fs.ID()
	if id == "" || len(id) > r.harness.session.limits.MaxStringBytes || strings.ContainsAny(id, "\r\n") {
		return nil, reject("invalid codemode output filesystem identity")
	}
	var directory, filesystemID string
	if err := r.harness.session.readTasks(ctx, func(state Snapshot) error {
		for _, doc := range state.Documents {
			if doc.Kind == codemodeRunKind && doc.Owner == r.taskID && doc.Key == key {
				var run codemodeRunState
				if err := fromObject(doc.Value, &run, r.harness.session.limits); err != nil {
					return err
				}
				directory, filesystemID = run.OutputDir, run.OutputFS
				return nil
			}
		}
		return reject("codemode output run missing")
	}); err != nil {
		return nil, err
	}
	if directory != "" {
		if filesystemID != id {
			return nil, reject("codemode output filesystem changed")
		}
		return &codemodeFSWriter{FileSystem: fs, directory: directory}, nil
	}
	created, err := fs.CreateTempDir(ctx, "pi-codemode-")
	if err != nil {
		return nil, err
	}
	if created == "" || len(created) > r.harness.session.limits.MaxStringBytes || strings.ContainsAny(created, "\r\n") {
		return nil, reject("invalid codemode output directory")
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	err = r.Commit(ctx, func(tx *Tx, _ TaskRecord) (*TaskState, error) {
		for _, doc := range tx.state.Documents {
			if doc.Kind != codemodeRunKind || doc.Owner != r.taskID || doc.Key != key {
				continue
			}
			var run codemodeRunState
			if err := fromObject(doc.Value, &run, tx.limits); err != nil {
				return nil, err
			}
			if run.OutputDir != "" {
				directory, filesystemID = run.OutputDir, run.OutputFS
				return nil, nil
			}
			run.OutputDir, run.OutputFS = created, id
			value, err := dtoObject(run, tx.limits)
			if err != nil {
				return nil, err
			}
			handle, err := tx.Document(doc.ID)
			if err != nil {
				return nil, err
			}
			if err = handle.Update(func(current JSON) error {
				for key := range current {
					delete(current, key)
				}
				for key, v := range value {
					current[key] = v
				}
				return nil
			}); err != nil {
				return nil, err
			}
			directory, filesystemID = created, id
			return nil, nil
		}
		return nil, reject("codemode output run missing")
	})
	// Do not delete on uncertain persistence: the directory may already be durable.
	if err != nil {
		return nil, err
	}
	if filesystemID != id {
		return nil, reject("codemode output filesystem changed")
	}
	return &codemodeFSWriter{FileSystem: fs, directory: directory}, nil
}

type codemodeFSWriter struct {
	FileSystem
	directory string
}

func (w *codemodeFSWriter) WriteCodemodeOutput(ctx context.Context, _ ID, _ string, name string, data []byte) (string, error) {
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return "", reject("invalid codemode output filename")
	}
	path, err := w.JoinPath(ctx, w.directory, name)
	if err != nil {
		return "", err
	}
	if err = w.WriteFile(ctx, path, data); err != nil {
		return "", err
	}
	return path, nil
}
