package conventions

import (
	"context"
	"strings"

	"actionlint.kjanat.dev/v2/internal/checking/rules/reporting"

	actioncheck "actionlint.kjanat.dev/v2/internal/checking/rules/correctness/action"

	workflowvisit "actionlint.kjanat.dev/v2/internal/checking/traversal"
	"actionlint.kjanat.dev/v2/internal/input/references/actions"

	actionremote "actionlint.kjanat.dev/v2/internal/input/references/actions/remote"

	workflowdecl "actionlint.kjanat.dev/v2/internal/model/workflows"

	findingmodel "actionlint.kjanat.dev/v2/internal/results/finding"
)

type ruleRequireActionMetadata struct {
	workflowvisit.Base
	remote *actionremote.ResolvedActions
	ctx    context.Context
}

// NewRuleRequireActionMetadata reports remote action metadata that could not be inspected.
func NewRuleRequireActionMetadata(ctx context.Context, remote *actionremote.ResolvedActions) workflowvisit.Rule {
	descriptor := MissingActionMetadataDeclaration()
	return &ruleRequireActionMetadata{Base: workflowvisit.NewBase(descriptor.Name, descriptor.Description), remote: remote, ctx: ctx}
}

func (r *ruleRequireActionMetadata) VisitStep(step workflowdecl.StepView) error {
	action, ok := step.Action()
	if !ok || !action.Uses().Present() || action.Uses().ContainsExpression() {
		return nil
	}
	spec := action.Uses().Value()
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "$/") || strings.HasPrefix(spec, "docker://") || len(actioncheck.RepositoryReference(spec, reporting.ViewPosition(action.Uses().Pos()))) != 0 {
		return nil
	}
	if r.ctx != nil {
		if err := r.ctx.Err(); err != nil {
			return err
		}
	}
	result := r.remote.Inspect(spec)
	availability := result.Availability()
	if availability.State() == actions.MetadataAvailable {
		return nil
	}
	reason := string(availability.Reason())
	if availability.State() == actions.MetadataInvalid {
		reason = "invalid parse/schema metadata"
	}
	r.Report(findingmodel.At(reporting.ViewPosition(action.Uses().Pos()), "missing-action-metadata", "remote action metadata for "+spec+" is unavailable ("+reason+"); use --action-metadata=online with a full SHA-1, or provide the required action-profile selection and warm the offline cache"))
	return nil
}
