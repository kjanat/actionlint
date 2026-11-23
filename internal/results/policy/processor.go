package policy

import "actionlint.kjanat.dev/v2/internal/input/configuration"

type Processor struct {
	RuleSet configuration.RuleSet
	// CanonicalPaths holds identities resolved during input capture. Result
	// processing never resolves paths on disk.
	CanonicalPaths map[string]string
}
