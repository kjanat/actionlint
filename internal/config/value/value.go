// Package value carries validated configuration across the public API boundary.
package value

import (
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"github.com/kjanat/go-yaml/v4"
)

// Document owns validated settings. Its zero value selects ordinary defaults.
// Construct documents with config.Parse or config.Load.
type Document struct{ config *repoconfig.Config }

// Wrap transfers ownership from the configuration loader to an opaque document.
func Wrap(config *repoconfig.Config) *Document {
	if config == nil {
		return nil
	}
	return &Document{config: config}
}

// Unwrap is available only to in-module adapters. Public callers cannot mutate
// the validated representation through Document.
func Unwrap(document *Document) *repoconfig.Config {
	if document == nil {
		return nil
	}
	if document.config == nil {
		return &repoconfig.Config{}
	}
	return document.config
}

// MarshalText returns a newly encoded configuration document.
func (d Document) MarshalText() ([]byte, error) { return yaml.Marshal(Unwrap(&d)) }
