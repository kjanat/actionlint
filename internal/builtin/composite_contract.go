package builtin

import (
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	repoconfig "actionlint.kjanat.dev/v2/internal/config"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// Invocation supplies the already-resolved caller and action scope.
// Participants own analyzer-specific configuration and state adaptation.
type Invocation struct {
	Context    invocation.Context
	ActionPath string
	Config     *repoconfig.Config
	Path       string
	Source     documentparse.SourceSnapshot
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
		CheckStandaloneMetadata(*actionmeta.Metadata, documentparse.SourceSnapshot) error
		ForMetadata(*actionlocal.Resolver) workflowvisit.Rule
	}
)

var _ InvocationParticipant = (*ruleExecutableBit)(nil)
