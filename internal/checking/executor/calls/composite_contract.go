package calls

import (
	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	repoconfig "actionlint.kjanat.dev/v2/internal/input/configuration"
	actionmeta "actionlint.kjanat.dev/v2/internal/model/actions"
	actionlocal "actionlint.kjanat.dev/v2/internal/model/actions/local"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// Invocation supplies the already-resolved caller and action scope.
// Participants own analyzer-specific configuration and state adaptation.
type Invocation struct {
	Context    invocation.Context
	ActionPath string
	Config     *repoconfig.Config
	Path       string
	Source     parsing.SourceSnapshot
}

// InvocationParticipant observes resolved action execution without producing child rules.
type InvocationParticipant interface {
	workflowvisit.Rule
	EnterInvocation(Invocation) error
	VisitInvocationStep(workflowdecl.StepView) error
	VisitOpaqueAction(workflowdecl.StepView) error
	LeaveInvocation(invocation.Context) error
}

// CheckoutParticipant adapts its own script paths to the checkout placement
// that actions resolved for the current invocation.
type CheckoutParticipant interface{ ApplyCheckout(*actionlocal.Resolver) }

type (
	MetadataParticipant interface {
		workflowvisit.Rule
		CheckStandaloneMetadata(*actionmeta.Metadata, parsing.SourceSnapshot) error
		ForMetadata(*actionlocal.Resolver) workflowvisit.Rule
	}
)
