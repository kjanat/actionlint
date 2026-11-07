// Package actionfixtures supplies captured remote manifests owned by the action integration suite.
package actionfixtures

import "embed"

// Manifests contains provenance headers and action metadata captured from remote repositories.
//
//go:embed testdata/remote-actions/*.yml
var Manifests embed.FS

// ReadManifest reads a captured manifest and its provenance header by fixture filename.
func ReadManifest(name string) ([]byte, error) {
	return Manifests.ReadFile("testdata/remote-actions/" + name)
}
