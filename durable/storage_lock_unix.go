//go:build unix

package durable

import (
	"errors"
	"os"
	"syscall"
)

// Lock the database inode for the entire native storage lifetime. NOFOLLOW
// prevents an existing file symlink from bypassing canonical ownership checks.
func lockStorageFile(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		file.Close()
		return nil, reject("storage file must be regular and owner-only")
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrOwned
		}
		return nil, err
	}
	return file, nil
}
