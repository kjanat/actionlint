package workflowvisit

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/workspace"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

// Invocation supplies the already-resolved caller and action scope.
// Participants own analyzer-specific configuration and state adaptation.
type Invocation struct {
	Bindings   *rulebinding.Scope
	Context    invocation.Context
	ActionPath string

	Path    string
	Source  parsing.SourceSnapshot
	Actions workspace.ActionMetadataLookup
}

// InvocationParticipant observes resolved action execution without producing child rules.
type InvocationParticipant interface {
	Rule
	EnterInvocation(Invocation) error
	VisitInvocationStep(workflows.Step) error
	VisitOpaqueAction(workflows.Step) error
	LeaveInvocation(invocation.Context) error
}

// ExpandedActionParticipant checks the call site of an expanded child before
// entering that child's invocation. Opaque call sites use VisitOpaqueAction.
type ExpandedActionParticipant interface {
	InvocationParticipant
	VisitExpandedAction(workflows.Step) error
}

// CheckoutParticipant adapts its own script paths to the checkout placement
// that actions resolved for the current invocation.
type CheckoutParticipant interface {
	ApplyCheckout(*workspace.Resolver)
}

type (
	MetadataParticipant interface {
		Rule
		CheckStandaloneMetadata(*actionmeta.Metadata, parsing.SourceSnapshot, actionmeta.ReferencedFiles) error
		ForMetadata(*workspace.Resolver) MetadataParticipant
	}
)
