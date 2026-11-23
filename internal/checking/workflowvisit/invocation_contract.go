package workflowvisit

import (
	"actionlint.kjanat.dev/v2/internal/input/configuration/rulebinding"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/localactions"
	"actionlint.kjanat.dev/v2/internal/model/actionmeta"
	"actionlint.kjanat.dev/v2/internal/model/calls/invocation"
	"actionlint.kjanat.dev/v2/internal/model/parsing"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

type Invocation struct {
	Bindings   *rulebinding.Scope
	Context    invocation.Context
	ActionPath string

	Path    string
	Source  parsing.SourceSnapshot
	Actions localactions.ActionMetadataLookup
}

type InvocationParticipant interface {
	Rule
	EnterInvocation(Invocation) error
	VisitInvocationStep(workflows.BoundStep) error
	VisitOpaqueAction(workflows.BoundStep) error
	LeaveInvocation(invocation.Context) error
}

// ExpandedActionParticipant runs before the child invocation, and opaque call sites use VisitOpaqueAction.
type ExpandedActionParticipant interface {
	InvocationParticipant
	VisitExpandedAction(workflows.BoundStep) error
}

type CheckoutParticipant interface {
	ApplyCheckout(*localactions.Resolver)
}

type (
	MetadataParticipant interface {
		Rule
		CheckStandaloneMetadata(*actionmeta.Metadata, parsing.SourceSnapshot, actionmeta.ReferencedFiles) error
		ForMetadata(*localactions.Resolver) MetadataParticipant
	}
)
