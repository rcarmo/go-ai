package tools

import (
	"context"
	"github.com/rcarmo/go-ai/durable"
	"golang.org/x/text/unicode/norm"
	"regexp"
	"strings"
)

// These rules follow the pinned tools/path-utils.ts transformations.
func normalizeToolPath(path string) string {
	path = strings.Map(func(r rune) rune {
		if r == 0xa0 || r >= 0x2000 && r <= 0x200a || r == 0x202f || r == 0x205f || r == 0x3000 {
			return ' '
		}
		return r
	}, path)
	return strings.TrimPrefix(path, "@")
}
func resolveFSPath(ctx context.Context, fs durable.FileSystem, path string) (string, error) {
	return fs.AbsolutePath(ctx, normalizeToolPath(path))
}

var meridiemPath = regexp.MustCompile(`(?i) (AM|PM)\.`)

func (e *Env) resolveReadToolPath(path string) (string, error) {
	return resolveFSReadPath(context.Background(), e, path)
}
func resolveFSReadPath(ctx context.Context, fs durable.FileSystem, path string) (string, error) {
	resolved, err := resolveFSPath(ctx, fs, path)
	if err != nil {
		return "", err
	}
	meridiem := meridiemPath.ReplaceAllString(resolved, "\u202f$1.")
	nfd := norm.NFD.String(resolved)
	for _, variant := range []string{resolved, meridiem, nfd, strings.ReplaceAll(resolved, "'", "’"), strings.ReplaceAll(nfd, "'", "’")} {
		exists, err := fs.Exists(ctx, variant)
		if err != nil {
			return "", err
		}
		if exists {
			return variant, nil
		}
	}
	return resolved, nil
}
