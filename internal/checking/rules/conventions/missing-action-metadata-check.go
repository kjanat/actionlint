package conventions

import (
	"strings"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"

	"actionlint.kjanat.dev/v2/internal/model/calls/references"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"
	factavailability "actionlint.kjanat.dev/v2/internal/model/availability"

	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"
)

// MissingActionMetadataCheck reads acquisition outcomes prepared by the host.
// It does not start provider requests or depend on preceding step callbacks.
func MissingActionMetadataCheck(remote *actionremote.ResolvedActions) checkinputs.DeclarationCheck {
	return checkinputs.DeclarationCheck{Declaration: MissingActionMetadataDeclaration(), Step: func(step workflowdecl.StepView) []reporting.Finding {
		return missingActionMetadata(step, remote)
	}}
}

func missingActionMetadata(step workflowdecl.StepView, remote *actionremote.ResolvedActions) []reporting.Finding {
	action, ok := step.Action()
	if !ok || !action.Uses().Present() || action.Uses().ContainsExpression() {
		return nil
	}
	spec := action.Uses().Value()
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "$/") || strings.HasPrefix(spec, "docker://") || references.ActionReferenceProblem(spec) != "" {
		return nil
	}
	result := remote.Inspect(spec)
	availability := result.Availability()
	if availability.State() == actions.MetadataAvailable {
		return nil
	}
	if reason := availability.Reason(); reason == factavailability.LookupDisabledReason || reason == factavailability.OfflineReason || reason == factavailability.CacheMissReason || reason == factavailability.ResolverNotConfiguredReason {
		return nil
	}
	reason := string(availability.Reason())
	if availability.State() == actions.MetadataInvalid {
		reason = "invalid parse/schema metadata"
	}
	help := ""
	switch availability.Reason() {
	case factavailability.UnsupportedHostReason:
		help = "; the GitHub metadata provider only supports github.com"
	case factavailability.UnsupportedObjectFormatReason:
		help = "; SHA-256 Git object IDs are not supported; use a full SHA-1 commit or a named reference with --resolve-with"
	case factavailability.InvalidResolverResultReason:
		help = "; the resolver returned an invalid or incomplete result; check the resolver selected by --resolve-with"
	}
	collector := reporting.Collector{Rule: "missing-action-metadata", Step: step}
	collector.Error(reporting.ViewPosition(action.Uses().Pos()), "could not inspect metadata for "+spec+" ("+reason+")"+help)
	return collector.Findings
}
