// Package guestlimits bounds the time and linear memory of WebAssembly
// guests. Component plugins and core module engines resolve their limits
// here, each with its own default memory size.
package guestlimits

import (
	"fmt"
	"time"
)

const (
	// DefaultTimeout bounds one guest call when the caller requests no timeout.
	DefaultTimeout = 10 * time.Second
	// MaxMemoryPages is the WebAssembly maximum of 64 KiB linear-memory pages.
	MaxMemoryPages = 65536
)

// Limits bounds one guest. As requested limits, zero values select defaults.
type Limits struct {
	Timeout     time.Duration
	MemoryPages uint32
}

func (l Limits) String() string {
	return fmt.Sprintf("memory %d MiB, time %s", uint64(l.MemoryPages)*64/1024, l.Timeout)
}

// Resolve validates requested limits before any artifact is read or runtime
// created, and fills a zero timeout with DefaultTimeout and zero memory with
// defaultMemoryPages.
func (requested Limits) Resolve(defaultMemoryPages uint32) (Limits, error) {
	if requested.MemoryPages > MaxMemoryPages {
		return Limits{}, fmt.Errorf("memory limit %d exceeds WebAssembly maximum of %d pages", requested.MemoryPages, MaxMemoryPages)
	}
	limits := requested
	if limits.Timeout <= 0 {
		limits.Timeout = DefaultTimeout
	}
	if limits.MemoryPages == 0 {
		limits.MemoryPages = defaultMemoryPages
	}
	return limits, nil
}
