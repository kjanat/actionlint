package engine

import (
	"actionlint.kjanat.dev/internal/analyzerhost"
)

// resourceParticipant is separate from the traversal and diagnostic contracts.
// Adapters register resources before the first visit so early exits still join.
type resourceParticipant interface{ attachScope(*analyzerhost.Scope) }
