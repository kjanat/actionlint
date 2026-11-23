// Package fixtures supplies captured remote manifests owned by the action integration suite.
package fixtures

import "embed"

//go:embed testdata/remote-actions/*.yml
var Manifests embed.FS

func ReadManifest(name string) ([]byte, error) {
	return Manifests.ReadFile("testdata/remote-actions/" + name)
}
