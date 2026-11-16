package conventions

import (
	"actionlint.kjanat.dev/v2/internal/checking/define"
	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	"actionlint.kjanat.dev/v2/internal/input/references/actions/remote"
	"actionlint.kjanat.dev/v2/internal/model/availability"
	"actionlint.kjanat.dev/v2/internal/model/calls/callreferences"
	"actionlint.kjanat.dev/v2/internal/model/rule"
	"actionlint.kjanat.dev/v2/internal/model/workflows"
)

// MissingActionMetadataRule is the single definition of the missing-action-metadata rule.
var MissingActionMetadataRule = &define.Rule{
	Name:        "missing-action-metadata",
	Aliases:     []rule.Alias{{Name: "require-action-metadata", Deprecated: true}},
	Description: "Require inspected metadata for remote actions",
	Category:    "conventions",
	Subcategory: "dependencies",
	LegacyGroup: "policy",
	Policy:      "require-action-metadata",
	Explanation: "Report remote action metadata that could not be inspected. Disabled downloads and offline cache misses are recorded in coverage without a lint finding.",
	Help:        "Check access to the referenced repository and its action.yml or action.yaml.",
	Checks:      func(c define.RuleInputs) define.Checks { return MissingActionMetadataChecks(c.RemoteActions) },
	Enabled: func(c define.RuleInputs) bool {
		return c.Config != nil && c.Config.Policy.RequireActionMetadata != nil && *c.Config.Policy.RequireActionMetadata
	},
}

// MissingActionMetadataChecks reads acquisition outcomes prepared by the host.
// It does not start provider requests or depend on preceding step callbacks.
func MissingActionMetadataChecks(remote *remote.ResolvedActions) define.Checks {
	return define.Checks{Step: func(step workflows.Step) []reporting.StepFinding {
		return missingActionMetadata(step, remote)
	}}
}

func missingActionMetadata(step workflows.Step, remote *remote.ResolvedActions) []reporting.StepFinding {
	action, ok := step.Action()
	if !ok || action.Uses == nil || action.Uses.ContainsExpression() {
		return nil
	}
	spec := action.Uses.Value
	switch callreferences.FormOf(spec) {
	case callreferences.FormWorkspace, callreferences.FormSelfRepository, callreferences.FormContainer:
		return nil
	case callreferences.FormRepository, callreferences.FormBuiltin:
	}
	if callreferences.ActionReferenceProblem(spec) != "" {
		return nil
	}
	result := remote.Inspect(spec)
	availabilityFacts := result.Availability()
	if availabilityFacts.State() == actions.MetadataAvailable {
		return nil
	}
	if reason := availabilityFacts.Reason(); reason == availability.LookupDisabledReason || reason == availability.OfflineReason || reason == availability.CacheMissReason || reason == availability.ResolverNotConfiguredReason {
		return nil
	}
	reason := string(availabilityFacts.Reason())
	if availabilityFacts.State() == actions.MetadataInvalid {
		reason = "invalid parse/schema metadata"
	}
	help := ""
	switch availabilityFacts.Reason() {
	case availability.UnsupportedHostReason:
		help = "; the GitHub metadata provider only supports github.com"
	case availability.UnsupportedObjectFormatReason:
		help = "; metadata lookup supports SHA-1 commit IDs; use a branch or tag name instead"
	case availability.InvalidResolverResultReason:
		help = "; the reference resolver returned an invalid or incomplete selection"
	case availability.NotFetchedReason, availability.OfflineReason, availability.LookupDisabledReason,
		availability.DeniedReason, availability.FailedReason, availability.NotFoundReason,
		availability.UnsupportedReason, availability.ResolverNotConfiguredReason, availability.CacheMissReason,
		availability.InaccessibleReason, availability.LimitReason:
	}
	collector := reporting.Collector{Step: step}
	collector.Error(action.Uses.Pos, "could not inspect metadata for "+spec+" ("+reason+")"+help)
	return collector.Findings
}
