package goai

import (
	"errors"
	"fmt"
	"sync"
)

var (
	sessionResourceCleanupsMu sync.Mutex
	sessionResourceCleanups   []func(sessionID string) error
)

// RegisterSessionResourceCleanup registers a cleanup callback for resources
// keyed by SessionID. It returns an unregister function.
func RegisterSessionResourceCleanup(cleanup func(sessionID string) error) func() {
	if cleanup == nil {
		return func() {}
	}
	sessionResourceCleanupsMu.Lock()
	sessionResourceCleanups = append(sessionResourceCleanups, cleanup)
	idx := len(sessionResourceCleanups) - 1
	sessionResourceCleanupsMu.Unlock()
	return func() {
		sessionResourceCleanupsMu.Lock()
		defer sessionResourceCleanupsMu.Unlock()
		if idx >= 0 && idx < len(sessionResourceCleanups) {
			sessionResourceCleanups[idx] = nil
		}
	}
}

// CleanupSessionResources closes registered resources for one session, or all
// sessions when sessionID is empty.
func CleanupSessionResources(sessionID string) error {
	sessionResourceCleanupsMu.Lock()
	cleanups := append([]func(sessionID string) error(nil), sessionResourceCleanups...)
	sessionResourceCleanupsMu.Unlock()

	var errs []error
	for _, cleanup := range cleanups {
		if cleanup == nil {
			continue
		}
		if err := invokeSessionCleanup(cleanup, sessionID); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func invokeSessionCleanup(cleanup func(string) error, sessionID string) (err error) {
	defer func() {
		if value := recover(); value != nil {
			if cause, ok := value.(error); ok {
				err = fmt.Errorf("session resource cleanup panic: %w", cause)
			} else {
				err = fmt.Errorf("session resource cleanup panic: %v", value)
			}
		}
	}()
	return cleanup(sessionID)
}
