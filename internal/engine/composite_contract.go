package engine

import (
	actionmeta "actionlint.kjanat.dev/internal/action"
	actionlocal "actionlint.kjanat.dev/internal/action/local"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

// compositeInvocation supplies the already-resolved caller and action scope.
// Participants own analyzer-specific configuration and state adaptation.
type compositeInvocation struct {
	call       *workflowlang.Step
	actionPath string
	actions    *actionlocal.Resolver
}

type compositeParticipant interface {
	workflowvisit.Rule
	forComposite(compositeInvocation) (workflowvisit.Rule, error)
}

type (
	checkoutParticipant interface{ checkoutPaths() *execution.Paths }
	compositeJoiner     interface {
		joinCompositeResult(*workflowlang.Step, workflowvisit.Rule)
	}
)
type (
	invocationParticipant interface{ deferActionInvocation() bool }
	metadataParticipant   interface {
		workflowvisit.Rule
		checkStandaloneMetadata(*actionmeta.Metadata)
		forMetadata(*actionlocal.Resolver) workflowvisit.Rule
	}
)

var _ compositeParticipant = (*RuleExecutableBit)(nil)
