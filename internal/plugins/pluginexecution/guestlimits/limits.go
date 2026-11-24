// Package guestlimits bounds the time and linear memory of WebAssembly
// guests.
package guestlimits

import (
	"fmt"
	"time"
)

const (
	DefaultTimeout = 10 * time.Second
	// MaxMemoryPages is the WebAssembly maximum of 64 KiB linear-memory pages.
	MaxMemoryPages = 65536
)

// Limits bounds one guest. Zero values select defaults.
type Limits struct {
	Timeout     time.Duration
	MemoryPages uint32
}

func (l Limits) String() string {
	return fmt.Sprintf("memory %d MiB, time %s", uint64(l.MemoryPages)*64/1024, l.Timeout)
}

// Resolve validates l before any artifact is read or runtime created, and
// fills zero values with defaults.
func (l Limits) Resolve(defaultMemoryPages uint32) (Limits, error) {
	if l.MemoryPages > MaxMemoryPages {
		return Limits{}, fmt.Errorf("memory limit %d exceeds WebAssembly maximum of %d pages", l.MemoryPages, MaxMemoryPages)
	}
	limits := l
	if limits.Timeout <= 0 {
		limits.Timeout = DefaultTimeout
	}
	if limits.MemoryPages == 0 {
		limits.MemoryPages = defaultMemoryPages
	}
	return limits, nil
}
