package durable

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

// CodemodeOutputWriter is an explicit environment capability. Implementations
// must confine writes to their owned project/run root, return stable paths for
// the same task/key/name, overwrite idempotently and honour cancellation.
// The environment owns file lifetime; the runtime never removes these files.
type CodemodeOutputWriter interface {
	ExecutionEnvironment
	WriteCodemodeOutput(context.Context, ID, string, string, []byte) (string, error)
}

func callCodemodeOutputWriter(ctx context.Context, writer CodemodeOutputWriter, task ID, key, name string, data []byte) (path string, err error) {
	defer func() {
		if recover() != nil {
			path = ""
			err = reject("codemode output writer panic")
		}
	}()
	return writer.WriteCodemodeOutput(ctx, task, key, name, data)
}
func (r *TaskRuntime) prepareCodemodeOutput(ctx context.Context, key string, result CodemodeResult, tokens int) (CodemodeResult, error) {
	if err := ctx.Err(); err != nil {
		return result, err
	}
	text := result.Output
	truncated := boundCodemodeOutput(text, tokens) != text
	if !truncated && len(result.Images) == 0 {
		return result, nil
	}
	environment, err := r.Environment(ctx)
	if err != nil {
		return result, err
	}
	writer, _ := environment.(CodemodeOutputWriter)
	write := func(name string, data []byte) (string, error) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if writer == nil {
			return "", reject("no codemode output writer")
		}
		path, err := callCodemodeOutputWriter(ctx, writer, r.taskID, key, name, data)
		if contextErr := ctx.Err(); contextErr != nil {
			return "", contextErr
		}
		if err != nil {
			r.Report(err)
			return "", reject("codemode output write failed")
		}
		if path == "" || len(path) > r.harness.session.limits.MaxStringBytes || strings.ContainsAny(path, "\r\n") {
			return "", reject("invalid codemode output path")
		}
		return path, nil
	}
	labels := []string{}
	saved := map[string]string{}
	for _, image := range result.Images {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		label, found := saved[image.Data]
		if !found {
			raw, err := base64.StdEncoding.Strict().DecodeString(image.Data)
			if err != nil {
				return result, reject("invalid codemode output image")
			}
			extension := map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/gif": ".gif", "image/webp": ".webp"}[image.MimeType]
			path, err := write(fmt.Sprintf("image-%d%s", len(saved)+1, extension), raw)
			if err != nil {
				label = fmt.Sprintf("[Image (%s, %d bytes) could not be saved]", image.MimeType, len(raw))
			} else {
				label = fmt.Sprintf("[Image saved to %s (%s, %d bytes)]", path, image.MimeType, len(raw))
			}
			saved[image.Data] = label
		}
		labels = append(labels, label)
	}
	if len(labels) > 0 {
		text = strings.Join(labels, "\n") + "\n" + text
	}
	if err := validateCodemodeFullText(text); err != nil {
		return result, err
	}
	truncated = boundCodemodeOutput(text, tokens) != text
	if truncated {
		path, err := write("output.txt", []byte(text))
		if err == nil {
			result.FullOutputPath = path
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Output = codemodeOutputWithPath(text, result.FullOutputPath, tokens)
	return result, nil
}
