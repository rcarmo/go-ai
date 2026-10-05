package durable

// Embedded native backends share the validation, serial admission and public
// detach boundary. Wrappers still retain their own Snapshot behaviour.
func (c *storeCore) nativeCore() *storeCore { return c }

type nativeStorage interface {
	Storage
	nativeCore() *storeCore
}
