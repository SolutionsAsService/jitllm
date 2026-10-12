//go:build linux || darwin

package gguf

import (
	"os"
	"syscall"
	"unsafe"
)

// reserve maps n bytes of anonymous memory with no commitment: an untouched
// page costs nothing, so a reservation the size of a 40 GB model is free.
func reserve(n int) ([]byte, error) {
	return syscall.Mmap(-1, 0, n, syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANON|syscall.MAP_NORESERVE)
}

func unreserve(b []byte) error { return syscall.Munmap(b) }

// PageSize is the unit Resident answers in.
func PageSize() int { return os.Getpagesize() }

// Resident reports, per page of b (which must start on a page), whether
// anything has read or written it.
func Resident(b []byte) ([]bool, error) {
	ps := PageSize()
	n := (len(b) + ps - 1) / ps
	if n == 0 {
		return nil, nil
	}
	vec := make([]byte, n)
	_, _, e := syscall.Syscall(syscall.SYS_MINCORE, uintptr(unsafe.Pointer(&b[0])),
		uintptr(len(b)), uintptr(unsafe.Pointer(&vec[0])))
	if e != 0 {
		return nil, e
	}
	out := make([]bool, n)
	for i, v := range vec {
		out[i] = v&1 != 0
	}
	return out, nil
}
