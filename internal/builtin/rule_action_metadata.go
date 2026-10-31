package builtin

import (
	"context"
	"strings"

	"actionlint.kjanat.dev/v2/actions"
	actionremote "actionlint.kjanat.dev/v2/internal/action/remote"
	actioncheck "actionlint.kjanat.dev/v2/internal/action/validation"
	findingmodel "actionlint.kjanat.dev/v2/internal/finding"
	workflowdecl "actionlint.kjanat.dev/v2/internal/workflow/declaration"
	workflowvisit "actionlint.kjanat.dev/v2/internal/workflow/visit"
)

type ruleRequireActionMetadata struct {
	workflowvisit.Base
	remote *actionremote.Service
	ctx    context.Context
}

func newRuleRequireActionMetadata(remote *actionremote.Service, ctx context.Context) *ruleRequireActionMetadata {
	if ctx == nil {
		ctx = context.Background()
	}
	return &ruleRequireActionMetadata{Base: builtinRuleBase("require-action-metadata"), remote: remote, ctx: ctx}
}

func (r *ruleRequireActionMetadata) VisitStep(step workflowdecl.StepView) error {
	action, ok := step.Action()
	if !ok || !action.Uses().Present() || action.Uses().ContainsExpression() {
		return nil
	}
	spec := action.Uses().Value()
	if strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "$/") || strings.HasPrefix(spec, "docker://") || len(actioncheck.RepositoryReference(spec, declarationPosition(action.Uses()))) != 0 {
		return nil
	}
	result, err := r.remote.Inspect(r.ctx, spec)
	if err != nil {
		return err
	}
	availability := result.Availability()
	if availability.State() == actions.MetadataAvailable {
		return nil
	}
	reason := string(availability.Reason())
	if availability.State() == actions.MetadataInvalid {
		reason = "invalid parse/schema metadata"
	}
	r.Report(findingmodel.At(declarationPosition(action.Uses()), "require-action-metadata", "remote action metadata for "+spec+" is unavailable ("+reason+"); use --action-metadata=online with a full SHA-1, or provide the required action-profile selection and warm the offline cache"))
	return nil
}
