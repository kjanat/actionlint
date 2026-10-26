package analyzer

import "actionlint.kjanat.dev/v2/diagnostic"

type Runtime struct{ Shell string }
type Invocation struct {
	Depth int
	Step  diagnostic.Position
}
