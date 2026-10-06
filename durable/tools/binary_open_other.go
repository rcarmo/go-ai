//go:build !unix

package tools

import "os"

func openRegularBinary(path string, noFollow bool) (*os.File, error) {
	if noFollow {
		info, err := os.Lstat(path)
		if err != nil {
			return nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, os.ErrInvalid
		}
	}
	return os.Open(path)
}
