package calls

import (
	"actionlint.kjanat.dev/v2/internal/checking/binding"
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	workspaceactions "actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// Invocation supplies the already-resolved caller and action scope.
// Participants own analyzer-specific configuration and state adaptation.
type Invocation struct {
	Bindings   *binding.Scope
	Context    invocation.Context
	ActionPath string

	Path    string
	Source  parsing.SourceSnapshot
	Actions workspaceactions.MetadataLookup
}

// InvocationParticipant observes resolved action execution without producing child rules.
type InvocationParticipant interface {
	workflowvisit.Rule
	EnterInvocation(Invocation) error
	VisitInvocationStep(workflowdecl.StepView) error
	VisitOpaqueAction(workflowdecl.StepView) error
	LeaveInvocation(invocation.Context) error
}

// ExpandedActionParticipant checks the call site of an expanded child before
// entering that child's invocation. Opaque call sites use VisitOpaqueAction.
type ExpandedActionParticipant interface {
	InvocationParticipant
	VisitExpandedAction(workflowdecl.StepView) error
}

// CheckoutParticipant adapts its own script paths to the checkout placement
// that actions resolved for the current invocation.
type CheckoutParticipant interface {
	ApplyCheckout(*workspaceactions.Resolver)
}

type (
	MetadataParticipant interface {
		workflowvisit.Rule
		CheckStandaloneMetadata(*actionmeta.Metadata, parsing.SourceSnapshot, actionmeta.ReferencedFiles) error
		ForMetadata(*workspaceactions.Resolver) MetadataParticipant
	}
)
