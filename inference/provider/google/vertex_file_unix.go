//go:build unix

package google

import (
	"os"
	"syscall"
)

// Opening with O_NONBLOCK prevents a path swapped to a FIFO from blocking
// before we can inspect the opened descriptor. Regular files keep their usual
// read semantics; the caller validates the descriptor before reading it.
func openVertexCredentialPath(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
}
