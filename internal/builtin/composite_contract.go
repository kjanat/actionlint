package builtin

import (
	actionmeta "actionlint.kjanat.dev/internal/action"
	actionlocal "actionlint.kjanat.dev/internal/action/local"
	workflowlang "actionlint.kjanat.dev/internal/workflow"
	"actionlint.kjanat.dev/internal/workflow/execution"
	workflowvisit "actionlint.kjanat.dev/internal/workflow/visit"
)

// Invocation supplies the already-resolved caller and action scope.
// Participants own analyzer-specific configuration and state adaptation.
type Invocation struct {
	Call       *workflowlang.Step
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
		JoinCompositeResult(*workflowlang.Step, workflowvisit.Rule)
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
