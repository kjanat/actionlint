package builtin

import (
	actionmeta "actionlint.kjanat.dev/v2/internal/action"
	actionlocal "actionlint.kjanat.dev/v2/internal/action/local"
	"actionlint.kjanat.dev/v2/internal/workflow/execution"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

// Invocation supplies the already-resolved caller and action scope.
// Participants own analyzer-specific configuration and state adaptation.
type Invocation struct {
	Context    execution.ActionContext
	ActionPath string
	Actions    *actionlocal.Resolver
}

type CompositeParticipant interface {
	workflowvisit.Rule
	ForComposite(Invocation) (workflowvisit.Rule, error)
}

type (
	CheckoutParticipant interface{ CheckoutPaths() *execution.Paths }
	CompositeJoiner     interface {
		JoinCompositeResult(execution.ActionContext, workflowvisit.Rule)
	}
)

type (
	InvocationParticipant interface{ DeferActionInvocation() bool }
	MetadataParticipant   interface {
		workflowvisit.Rule
		CheckStandaloneMetadata(*actionmeta.Metadata)
		ForMetadata(*actionlocal.Resolver) workflowvisit.Rule
	}
)

var _ CompositeParticipant = (*RuleExecutableBit)(nil)
