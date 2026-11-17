package guestlimits

import (
	"strings"
	"testing"
	"time"
)

func TestResolveFillsDefaultsAndRejectsOversizedMemory(t *testing.T) {
	t.Parallel()
	if got, err := (Limits{}).Resolve(1024); err != nil || got != (Limits{Timeout: DefaultTimeout, MemoryPages: 1024}) {
		t.Fatalf("zero limits resolved to %v, %v", got, err)
	}
	requested := Limits{Timeout: time.Second, MemoryPages: MaxMemoryPages}
	if got, err := requested.Resolve(1024); err != nil || got != requested {
		t.Fatalf("explicit limits resolved to %v, %v", got, err)
	}
	if _, err := (Limits{MemoryPages: MaxMemoryPages + 1}).Resolve(1024); err == nil || !strings.Contains(err.Error(), "65536 pages") {
		t.Fatalf("oversized memory accepted: %v", err)
	}
	if got := (Limits{Timeout: time.Second, MemoryPages: 1024}).String(); got != "memory 64 MiB, time 1s" {
		t.Fatalf("String() = %q", got)
	}
}
