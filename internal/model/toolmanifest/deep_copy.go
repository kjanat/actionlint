package toolmanifest

//go:generate go run ../../development/generators/deep-copies -root ../../.. -package ./internal/model/toolmanifest -type Manifest -output internal/model/toolmanifest/deep_copy.gen.go

// Clone returns a manifest that shares no mutable data with m.
func (m Manifest) Clone() Manifest { return deepCopyManifest(m) }
