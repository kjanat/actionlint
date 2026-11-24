package finding

//go:generate go run ../../development/generators/deep-copies -root ../../..

func (e *Finding) Clone() *Finding {
	clone := deepCopyFinding(*e)
	return &clone
}

// Copier must not be used concurrently, and its copies must treat their Source bytes as immutable.
type Copier struct{ context deepCopyContext }

func NewCopier() *Copier { return &Copier{} }

func (c *Copier) Clone(finding *Finding) *Finding {
	clone := c.context.deepCopyFinding(*finding)
	return &clone
}
