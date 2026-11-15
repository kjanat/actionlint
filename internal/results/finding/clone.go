package finding

//go:generate go run ../../development/generators/owned-copies -root ../../..

// Clone returns a deep copy of the finding. Annotating the copy, including its
// origins, fixes and foreign source, leaves the original unchanged.
func (e *Finding) Clone() *Finding {
	clone := copyOwnedFinding(*e)
	return &clone
}

// Copier detaches findings while sharing copies of identical source buffers.
// Findings copied by one Copier must treat their Source bytes as immutable.
// A Copier must not be used concurrently.
type Copier struct{ context ownedCopyContext }

// NewCopier creates an ownership boundary for a group of findings.
func NewCopier() *Copier { return &Copier{} }

// Clone returns a distinct finding with independently owned mutable metadata.
func (c *Copier) Clone(finding *Finding) *Finding {
	clone := c.context.copyOwnedFinding(*finding)
	return &clone
}
