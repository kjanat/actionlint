package typecheck

// Options nil name lists permit unknown names; empty lists allow no configured names.
type Options struct {
	Variables []string
	Secrets   []string
}
