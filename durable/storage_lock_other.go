//go:build !unix && cgo

package durable

import "os"

func lockStorageFile(path string) (*os.File, error) { return nil, ErrUnsupported }
