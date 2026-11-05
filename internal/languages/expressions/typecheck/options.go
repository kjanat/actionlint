package typecheck

// Options supplies analysis facts independently of repository config.
// Nil name lists permit unknown names; empty lists allow no configured names.
type Options struct {
	Variables []string
	Secrets   []string
}
