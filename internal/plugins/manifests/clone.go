package manifests

//go:generate go run ../../development/generators/owned-copies -root ../../.. -package ./internal/plugins/manifests -type Manifest -output internal/plugins/manifests/owned_copy.gen.go

// Clone detaches the manifest's mutable data from its caller.
func (m Manifest) Clone() Manifest { return copyOwnedManifest(m) }
