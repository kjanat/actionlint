package manifests

//go:generate go run ../../development/generators/owned-copies -root ../../.. -package ./internal/plugins/manifests -type Manifest -output internal/plugins/manifests/owned_copy.gen.go

// Clone returns a manifest that shares no mutable data with m.
func (m Manifest) Clone() Manifest { return copyOwnedManifest(m) }
