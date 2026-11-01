// Package value carries validated configuration across the public API boundary.
package value

import (
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
)

// Document owns validated settings. Its zero value selects ordinary defaults.
// Construct documents with config.Parse or config.Load.
type Document struct {
	config     *repoconfig.Config
	provenance repoconfig.Provenance
}

// Wrap transfers ownership from the configuration loader to an opaque document.
func Wrap(config *repoconfig.Config) *Document {
	if config == nil {
		return nil
	}
	return &Document{config: config}
}

// WrapLoaded retains the source owner when validated settings cross the API boundary.
func WrapLoaded(loaded *repoconfig.Loaded) *Document {
	if loaded == nil {
		return nil
	}
	return &Document{config: loaded.Config, provenance: loaded.Provenance}
}

// Provenance carries loaded source identity into in-module analysis adapters.
func Provenance(document *Document) repoconfig.Provenance {
	if document == nil {
		return repoconfig.Provenance{}
	}
	return document.provenance
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
func (d Document) MarshalText() ([]byte, error) { return repoconfig.Marshal(Unwrap(&d)) }
