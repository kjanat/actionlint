package engine

import (
	workflowlang "actionlint.kjanat.dev/internal/workflow"
)

// compositeInvocation supplies the already-resolved caller and action scope.
// Participants own analyzer-specific configuration and state adaptation.
type compositeInvocation struct {
	call       *workflowlang.Step
	actionPath string
	actions    *LocalActionsCache
}

type compositeParticipant interface {
	Rule
	forComposite(compositeInvocation) (Rule, error)
}

type (
	checkoutParticipant interface{ checkoutPaths() *runPaths }
	compositeJoiner     interface {
		joinCompositeResult(*workflowlang.Step, Rule)
	}
)
type (
	invocationParticipant interface{ deferActionInvocation() bool }
	metadataParticipant   interface {
		Rule
		checkStandaloneMetadata(*ActionMetadata)
		forMetadata(*LocalActionsCache) Rule
	}
)

var _ compositeParticipant = (*RuleExecutableBit)(nil)
