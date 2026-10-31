package cachepolicy

import (
	workflowlang "actionlint.kjanat.dev/v2/internal/workflow"
	"actionlint.kjanat.dev/v2/internal/workflow/calls"
	"actionlint.kjanat.dev/v2/internal/workflow/declaration"
)

// EffectiveMode selects the explicit job grant, otherwise the workflow grant.
func EffectiveMode(workflow, job declaration.CacheModeView) declaration.CacheModeView {
	if job.Present() {
		return job
	}
	return workflow
}

// modeGrant crosses the reusable metadata boundary without sharing mutable nodes.
// An absent mode inherits its caller; an invalid declaration stops policy cascades.
type modeGrant struct {
	present bool
	kind    workflowlang.CacheModeKind
}

func grant(mode declaration.CacheModeView) modeGrant { return modeGrant{mode.Present(), mode.Kind()} }

func metadataGrant(mode calls.CacheModeDeclaration) modeGrant {
	kind, declared := mode.Kind()
	return modeGrant{declared, kind}
}

func effectiveGrant(parent, child modeGrant) modeGrant {
	if child.present {
		return child
	}
	return parent
}

func (m modeGrant) capabilities() (uint8, bool) {
	if !m.present {
		return 0, false
	}
	return m.kind.Capabilities()
}

func position(node interface {
	Pos() (workflowlang.Pos, bool)
},
) workflowlang.Pos {
	pos, _ := node.Pos()
	return pos
}
