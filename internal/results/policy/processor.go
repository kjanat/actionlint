package policy

import repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"

// Processor applies rule metadata, resolved levels and suppressions to findings.
// The caller supplies the declarations used for the check being processed.
type Processor struct {
	RuleSet repoconfig.RuleSet
	// CanonicalPaths maps source and directory aliases to identities resolved
	// during input capture. Result processing never resolves paths on disk.
	CanonicalPaths map[string]string
}
