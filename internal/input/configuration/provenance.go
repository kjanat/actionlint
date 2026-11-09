package config

import "slices"

// Provenance records the root document and the sources that contributed to its
// configuration. Loaded documents and analysis sessions carry it.
type Provenance struct {
	path  string
	files []string
}

// Path returns the absolute local root filename or its HTTPS address.
func (p Provenance) Path() string { return p.path }

// Sources returns a detached list of contributing sources, bases first.
func (p Provenance) Sources() []string { return slices.Clone(p.files) }
