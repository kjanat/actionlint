package expression

// Options supplies analysis facts independently of repository config.
// Nil name lists permit unknown names; empty lists allow no configured names.
type Options struct {
	Variables []string
	Secrets   []string
	Checks    Checks
}

// Checks selects optional semantics without depending on rule presets.
type Checks struct {
	StringConditions          bool
	MixedTypeComparisons      bool
	CaseInsensitiveConditions bool
}
