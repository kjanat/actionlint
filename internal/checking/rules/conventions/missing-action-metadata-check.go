package conventions

import (
	"strings"

	checkinputs "actionlint.kjanat.dev/v2/internal/checking/inputs"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"

	"actionlint.kjanat.dev/v2/internal/model/calls/references"

	"actionlint.kjanat.dev/v2/internal/input/references/actions"

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
	reason := string(availability.Reason())
	if availability.State() == actions.MetadataInvalid {
		reason = "invalid parse/schema metadata"
	}
	collector := reporting.Collector{Rule: "missing-action-metadata", Step: step}
	collector.Error(reporting.ViewPosition(action.Uses().Pos()), "remote action metadata for "+spec+" is unavailable ("+reason+"); use --action-metadata=online with a full SHA-1, or provide the required action-profile selection and warm the offline cache")
	return collector.Findings
}
