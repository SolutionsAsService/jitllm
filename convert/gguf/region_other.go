//go:build !linux && !darwin

package gguf

func reserve(n int) ([]byte, error) { return nil, ErrNoStream }

func unreserve(b []byte) error { return nil }

// PageSize is the unit Resident answers in.
func PageSize() int { return 4096 }

// Resident is unavailable here; see Streamable.
func Resident(b []byte) ([]bool, error) { return nil, ErrNoStream }
