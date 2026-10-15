package engine

import "actionlint.kjanat.dev/internal/analyzerhost"

// resourceParticipant is separate from the traversal and diagnostic contracts.
// Adapters register resources before the first visit so early exits still join.
type resourceParticipant interface{ attachScope(*analyzerhost.Scope) }

func (rule *RuleShellcheck) attachScope(scope *analyzerhost.Scope) {
	if rule.cmd.proc == nil || rule.cmd.proc.scope == nil {
		scope.Register(rule.cmd.wait)
	}
	rule.hostManaged = true
}

func (r *ruffRule) attachScope(scope *analyzerhost.Scope) {
	if !r.hostManaged {
		scope.Register(r.checker.Wait)
	}
	r.hostManaged = true
}
