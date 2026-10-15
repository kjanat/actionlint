package engine

// compositeInvocation supplies the already-resolved caller and action scope.
// Participants own analyzer-specific configuration and state adaptation.
type compositeInvocation struct {
	call       *Step
	actionPath string
	actions    *LocalActionsCache
}

type compositeParticipant interface {
	Rule
	forComposite(compositeInvocation) (Rule, error)
}

type checkoutParticipant interface{ checkoutPaths() *runPaths }
type compositeJoiner interface{ joinCompositeResult(*Step, Rule) }
type invocationParticipant interface{ deferActionInvocation() bool }
type metadataParticipant interface {
	Rule
	checkStandaloneMetadata(*ActionMetadata)
	forMetadata(*LocalActionsCache) Rule
}

var _ compositeParticipant = (*RuleExecutableBit)(nil)
