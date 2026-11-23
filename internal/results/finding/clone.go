package finding

//go:generate go run ../../development/generators/owned-copies -root ../../..

func (e *Finding) Clone() *Finding {
	clone := copyOwnedFinding(*e)
	return &clone
}

// Copier must not be used concurrently, and its copies must treat their Source bytes as immutable.
type Copier struct{ context ownedCopyContext }

func NewCopier() *Copier { return &Copier{} }

func (c *Copier) Clone(finding *Finding) *Finding {
	clone := c.context.copyOwnedFinding(*finding)
	return &clone
}
