//go:build !unix

package google

import "os"

// These platforms do not expose Unix FIFO semantics. Keep the native opener;
// the common caller also validates the opened descriptor before reading it.
func openVertexCredentialPath(path string) (*os.File, error) {
	return os.Open(path)
}
