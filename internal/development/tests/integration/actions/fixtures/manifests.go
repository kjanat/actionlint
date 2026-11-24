// Package fixtures embeds captured remote action manifests for the action integration tests.
package fixtures

import "embed"

//go:embed testdata/remote-actions/*.yml
var Manifests embed.FS

func ReadManifest(name string) ([]byte, error) {
	return Manifests.ReadFile("testdata/remote-actions/" + name)
}
