package builtin

import (
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/documentparse"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	"actionlint.kjanat.dev/v2/internal/workflow/invocation"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// Invocation supplies the already-resolved caller and action scope.
// Participants own analyzer-specific configuration and state adaptation.
type Invocation struct {
	Context    invocation.Context
	ActionPath string
	Actions    *actionlocal.Resolver
	Source     *documentparse.SourceSnapshot
}

type CompositeParticipant interface {
	workflowvisit.Rule
	ForComposite(Invocation) (workflowvisit.Rule, error)
}

type (
	CheckoutParticipant interface{ CheckoutPaths() *execution.Paths }
	CompositeJoiner     interface {
		JoinCompositeResult(invocation.Context, workflowvisit.Rule)
	}
)

type (
	InvocationParticipant interface{ DeferActionInvocation() bool }
	MetadataParticipant   interface {
		workflowvisit.Rule
		CheckStandaloneMetadata(*actionmeta.Metadata, *documentparse.SourceSnapshot) error
		ForMetadata(*actionlocal.Resolver) workflowvisit.Rule
	}
)

var _ CompositeParticipant = (*RuleExecutableBit)(nil)
