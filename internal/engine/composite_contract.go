package engine

// compositeInvocation supplies the already-resolved caller and action scope.
// Participants own analyzer-specific configuration and state adaptation.
type compositeInvocation struct {
	call          *Step
	actionPath    string
	actions       *LocalActionsCache
	metadata      *ActionMetadata
	configForFile func(string) (*Config, error)
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

var (
	_ compositeParticipant = (*RuleDeprecatedCommands)(nil)
	_ compositeParticipant = (*RuleExecutableBit)(nil)
	_ compositeParticipant = (*RuleShellcheck)(nil)
	_ compositeParticipant = (*ruffRule)(nil)
)
