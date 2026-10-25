package component

import (
	"fmt"
	"time"

	pluginartifact "actionlint.kjanat.dev/v2/internal/plugins/artifact"
)

const (
	// DefaultTimeout bounds guest execution after caller-controlled queue admission.
	DefaultTimeout = 10 * time.Second
	// DefaultMemoryPages bounds live linear memory and reserved core tables.
	DefaultMemoryPages uint32 = 1024
	maxLiftBytes              = 8 * 1024 * 1024
	maxLiftValues             = 262144
)

// Options configures a loaded plugin. Zero values select the defaults.
type Options struct {
	// Artifact configures URI resolution and verified download caching.
	Artifact    pluginartifact.Options
	Timeout     time.Duration
	MemoryPages uint32
	// Log receives host log lines and the plugin's stdout and stderr lines.
	Log func(string)
}

// Limits reports the resource bounds a plugin runs under.
type Limits struct {
	Timeout     time.Duration
	MemoryPages uint32
}

func (l Limits) String() string {
	return fmt.Sprintf("memory %d MiB, time %s", uint64(l.MemoryPages)*64/1024, l.Timeout)
}

// limits validates resource policy before artifact resolution or runtime creation.
func (o Options) limits() (Limits, error) {
	if o.MemoryPages > 65536 {
		return Limits{}, fmt.Errorf("plugin memory limit %d exceeds WebAssembly maximum of 65536 pages", o.MemoryPages)
	}
	limits := Limits{Timeout: o.Timeout, MemoryPages: o.MemoryPages}
	if limits.Timeout <= 0 {
		limits.Timeout = DefaultTimeout
	}
	if limits.MemoryPages == 0 {
		limits.MemoryPages = DefaultMemoryPages
	}
	return limits, nil
}
