//go:build !cgo

package durable

func directSQLiteTaskSnapshot(Storage) bool { return false }
