package configuration

import (
	"maps"
	"slices"
)

// Provenance records the root document and the sources that contributed to its
// configuration. Loaded documents and analysis sessions carry it.
type Provenance struct {
	path    string
	files   []string
	digests map[string]string
}

// Path returns the absolute local root filename or its HTTPS address.
func (p Provenance) Path() string { return p.path }

// Sources returns a detached list of contributing sources, bases first.
func (p Provenance) Sources() []string { return slices.Clone(p.files) }

// Digests maps each source's normalized address to the lowercase hexadecimal
// SHA-256 digest of its content as loaded.
func (p Provenance) Digests() map[string]string { return maps.Clone(p.digests) }
