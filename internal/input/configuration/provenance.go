package config

import "slices"

// Provenance identifies the root document and every contributing source.
// It is carried by loaded documents and analysis sessions, not settings DTOs.
type Provenance struct {
	path  string
	files []string
}

// Path returns the absolute local root filename or its HTTPS address.
func (p Provenance) Path() string { return p.path }

// Sources returns a detached list of contributing sources, bases first.
func (p Provenance) Sources() []string { return slices.Clone(p.files) }
