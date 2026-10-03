package durable

import (
	"sync"
)

// MemoryImage retains a detached store image across close/reopen in one process.
// It provides no persistence after losing RAM. Ownership lasts until Close has
// settled admitted writes, not merely until a caller stops waiting for Close.
type MemoryImage struct {
	mu          sync.Mutex
	owned       bool
	state       Snapshot
	ordinal     uint64
	limits      Limits
	initialized bool
}
type MemoryOptions struct {
	Limits *Limits
	Image  *MemoryImage
}
type MemoryStorage struct {
	*storeCore
	image *MemoryImage
}

func OpenMemory(options MemoryOptions) (*MemoryStorage, error) {
	l := DefaultLimits()
	if options.Limits != nil {
		l = *options.Limits
	}
	if err := l.validate(); err != nil {
		return nil, err
	}
	image := options.Image
	if image == nil {
		image = &MemoryImage{}
	}
	image.mu.Lock()
	defer image.mu.Unlock()
	if image.owned {
		return nil, ErrOwned
	}
	c := newCore(l)
	if image.initialized {
		if options.Limits != nil && l != image.limits {
			return nil, reject("persisted memory limits conflict")
		}
		c.limits = image.limits
		var err error
		c.state, err = cloneState(image.state, c.limits)
		if err != nil {
			return nil, err
		}
		c.ordinal = image.ordinal
	}
	image.owned = true
	m := &MemoryStorage{storeCore: c, image: image}
	c.finish = func() error {
		image.mu.Lock()
		defer image.mu.Unlock()
		s, e := cloneState(c.state, c.limits)
		if e != nil {
			return e
		}
		image.state = s
		image.limits = c.limits
		image.ordinal = c.ordinal
		image.initialized = true
		image.owned = false
		return nil
	}
	return m, nil
}

// NewMemory is a default in-memory storage factory, equivalent to OpenMemory.
func NewMemory() (*MemoryStorage, error) { return OpenMemory(MemoryOptions{}) }

var _ Storage = (*MemoryStorage)(nil)
